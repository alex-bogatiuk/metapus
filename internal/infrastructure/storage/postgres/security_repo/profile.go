// Package security_repo provides PostgreSQL implementations for security profile storage.
package security_repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain/security_profile"
	"metapus/internal/infrastructure/storage/postgres"
)

// ProfileRepo implements security_profile.Repository using PostgreSQL.
type ProfileRepo struct{}

// NewProfileRepo creates a new ProfileRepo.
func NewProfileRepo() *ProfileRepo {
	return &ProfileRepo{}
}

// Builder returns a new squirrel PostgreSQL builder.
func (r *ProfileRepo) Builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *ProfileRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// ─── GetByID ─────────────────────────────────────────────────────────

func (r *ProfileRepo) GetByID(ctx context.Context, profileID id.ID) (*security_profile.SecurityProfile, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	// Load profile header
	profile := &security_profile.SecurityProfile{}
	q, args, err := r.Builder().
		Select("id", "code", "name", "description", "is_system", "created_at", "updated_at").
		From("security_profiles").
		Where(squirrel.Eq{"id": profileID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build profile query: %w", err)
	}

	if err := pgxscan.Get(ctx, querier, profile, q, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, apperror.NewNotFound("security_profiles", profileID.String())
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}

	// Load dimensions + field policies + policy rules
	if err := r.loadDimensions(ctx, querier, profile); err != nil {
		return nil, err
	}
	if err := r.loadFieldPolicies(ctx, querier, profile); err != nil {
		return nil, err
	}
	if err := r.loadPolicyRules(ctx, querier, profile); err != nil {
		return nil, err
	}

	// Load user count
	var userCount int
	if err := querier.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_security_profiles WHERE profile_id = $1`, profileID,
	).Scan(&userCount); err == nil {
		profile.UserCount = userCount
	}

	return profile, nil
}

// ─── GetByUserID ─────────────────────────────────────────────────────

func (r *ProfileRepo) GetByUserID(ctx context.Context, userID id.ID) (*security_profile.SecurityProfile, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	// Find user's profile(s) — pick the first one (or merge later).
	q, args, err := r.Builder().
		Select("sp.id", "sp.code", "sp.name", "sp.description", "sp.is_system", "sp.created_at", "sp.updated_at").
		From("security_profiles sp").
		Join("user_security_profiles usp ON usp.profile_id = sp.id").
		Where(squirrel.Eq{"usp.user_id": userID}).
		Limit(1).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build user profile query: %w", err)
	}

	profile := &security_profile.SecurityProfile{}
	if err := pgxscan.Get(ctx, querier, profile, q, args...); err != nil {
		if pgxscan.NotFound(err) {
			return nil, nil // No profile assigned — no restrictions from profile
		}
		return nil, fmt.Errorf("get user profile: %w", err)
	}

	if err := r.loadDimensions(ctx, querier, profile); err != nil {
		return nil, err
	}
	if err := r.loadFieldPolicies(ctx, querier, profile); err != nil {
		return nil, err
	}
	if err := r.loadPolicyRules(ctx, querier, profile); err != nil {
		return nil, err
	}

	return profile, nil
}

// ─── List ────────────────────────────────────────────────────────────

func (r *ProfileRepo) List(ctx context.Context) ([]*security_profile.SecurityProfile, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	q, args, err := r.Builder().
		Select("id", "code", "name", "description", "is_system", "created_at", "updated_at").
		From("security_profiles").
		OrderBy("name ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list query: %w", err)
	}

	var profiles []*security_profile.SecurityProfile
	if err := pgxscan.Select(ctx, querier, &profiles, q, args...); err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	// Load dimensions + field policies + policy rules for all profiles in batch
	if len(profiles) > 0 {
		profileIDs := make([]id.ID, 0, len(profiles))
		for _, p := range profiles {
			profileIDs = append(profileIDs, p.ID)
		}
		if err := r.loadDimensionsBatch(ctx, querier, profiles, profileIDs); err != nil {
			return nil, err
		}
		if err := r.loadFieldPoliciesBatch(ctx, querier, profiles, profileIDs); err != nil {
			return nil, err
		}
		if err := r.loadPolicyRulesBatch(ctx, querier, profiles, profileIDs); err != nil {
			return nil, err
		}
	}

	// Load user counts per profile
	userCounts, err := r.loadUserCounts(ctx, querier)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		p.UserCount = userCounts[p.ID.String()]
	}

	return profiles, nil
}

// ─── Create ──────────────────────────────────────────────────────────

func (r *ProfileRepo) Create(ctx context.Context, profile *security_profile.SecurityProfile) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	if id.IsNil(profile.ID) {
		profile.ID = id.New()
	}

	q, args, err := r.Builder().
		Insert("security_profiles").
		Columns("id", "code", "name", "description", "is_system").
		Values(profile.ID, profile.Code, profile.Name, profile.Description, profile.IsSystem).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert query: %w", err)
	}

	if _, err := querier.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}

	// Save dimensions
	if err := r.saveDimensions(ctx, querier, profile); err != nil {
		return err
	}

	// Save field policies
	if err := r.saveFieldPolicies(ctx, querier, profile); err != nil {
		return err
	}

	return nil
}

// ─── Update ──────────────────────────────────────────────────────────

func (r *ProfileRepo) Update(ctx context.Context, profile *security_profile.SecurityProfile) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	q, args, err := r.Builder().
		Update("security_profiles").
		Set("code", profile.Code).
		Set("name", profile.Name).
		Set("description", profile.Description).
		Set("updated_at", squirrel.Expr("now()")).
		Where(squirrel.Eq{"id": profile.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update query: %w", err)
	}

	if _, err := querier.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}

	// Replace dimensions (delete + insert)
	delQ, delArgs, _ := r.Builder().Delete("security_profile_dimensions").
		Where(squirrel.Eq{"profile_id": profile.ID}).ToSql()
	if _, err := querier.Exec(ctx, delQ, delArgs...); err != nil {
		return fmt.Errorf("delete old dimensions: %w", err)
	}
	if err := r.saveDimensions(ctx, querier, profile); err != nil {
		return err
	}

	// Replace field policies (delete + insert)
	delQ2, delArgs2, _ := r.Builder().Delete("security_profile_field_policies").
		Where(squirrel.Eq{"profile_id": profile.ID}).ToSql()
	if _, err := querier.Exec(ctx, delQ2, delArgs2...); err != nil {
		return fmt.Errorf("delete old field policies: %w", err)
	}
	if err := r.saveFieldPolicies(ctx, querier, profile); err != nil {
		return err
	}

	return nil
}

// ─── Delete ──────────────────────────────────────────────────────────

func (r *ProfileRepo) Delete(ctx context.Context, profileID id.ID) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	q, args, err := r.Builder().
		Delete("security_profiles").
		Where(squirrel.Eq{"id": profileID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete query: %w", err)
	}

	if _, err := querier.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// ─── AssignToUser / RemoveFromUser ───────────────────────────────────

func (r *ProfileRepo) AssignToUser(ctx context.Context, userID, profileID id.ID) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	q, args, err := r.Builder().
		Insert("user_security_profiles").
		Columns("user_id", "profile_id").
		Values(userID, profileID).
		Suffix("ON CONFLICT (user_id, profile_id) DO NOTHING").
		ToSql()
	if err != nil {
		return fmt.Errorf("build assign query: %w", err)
	}

	if _, err := querier.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("assign profile to user: %w", err)
	}
	return nil
}

func (r *ProfileRepo) RemoveFromUser(ctx context.Context, userID, profileID id.ID) error {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	q, args, err := r.Builder().
		Delete("user_security_profiles").
		Where(squirrel.Eq{"user_id": userID, "profile_id": profileID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build remove query: %w", err)
	}

	if _, err := querier.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("remove profile from user: %w", err)
	}
	return nil
}

// ─── Internal helpers ────────────────────────────────────────────────

func (r *ProfileRepo) loadDimensions(ctx context.Context, querier postgres.Querier, profile *security_profile.SecurityProfile) error {
	q, args, err := r.Builder().
		Select("dimension_name", "entity_name", "allowed_ids").
		From("security_profile_dimensions").
		Where(squirrel.Eq{"profile_id": profile.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build dimensions query: %w", err)
	}

	rows, err := querier.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query dimensions: %w", err)
	}
	defer rows.Close()

	globalDims := make(map[string][]string)
	entityDims := make(map[string]map[string][]string)

	for rows.Next() {
		var dimName, entityName string
		var allowedIDs []string
		if err := rows.Scan(&dimName, &entityName, &allowedIDs); err != nil {
			return fmt.Errorf("scan dimension: %w", err)
		}
		if entityName == "" {
			// Global dimension (applies to all entities)
			globalDims[dimName] = allowedIDs
		} else {
			// Per-entity dimension
			if entityDims[entityName] == nil {
				entityDims[entityName] = make(map[string][]string)
			}
			entityDims[entityName][dimName] = allowedIDs
		}
	}
	profile.Dimensions = globalDims
	if len(entityDims) > 0 {
		profile.EntityDimensions = entityDims
	}
	return rows.Err()
}

func (r *ProfileRepo) loadDimensionsBatch(ctx context.Context, querier postgres.Querier, profiles []*security_profile.SecurityProfile, profileIDs []id.ID) error {
	q, args, err := r.Builder().
		Select("profile_id", "dimension_name", "entity_name", "allowed_ids").
		From("security_profile_dimensions").
		Where(squirrel.Eq{"profile_id": profileIDs}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build dimensions query: %w", err)
	}

	rows, err := querier.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query dimensions: %w", err)
	}
	defer rows.Close()

	// maps: profile_id -> ...
	type profDims struct {
		global map[string][]string
		entity map[string]map[string][]string
	}
	dimsMap := make(map[id.ID]*profDims)
	for _, pid := range profileIDs {
		dimsMap[pid] = &profDims{
			global: make(map[string][]string),
			entity: make(map[string]map[string][]string),
		}
	}

	for rows.Next() {
		var profileID id.ID
		var dimName, entityName string
		var allowedIDs []string
		if err := rows.Scan(&profileID, &dimName, &entityName, &allowedIDs); err != nil {
			return fmt.Errorf("scan dimension: %w", err)
		}
		
		dMap, ok := dimsMap[profileID]
		if !ok {
			continue
		}

		if entityName == "" {
			dMap.global[dimName] = allowedIDs
		} else {
			if dMap.entity[entityName] == nil {
				dMap.entity[entityName] = make(map[string][]string)
			}
			dMap.entity[entityName][dimName] = allowedIDs
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		if dMap, ok := dimsMap[p.ID]; ok {
			p.Dimensions = dMap.global
			if len(dMap.entity) > 0 {
				p.EntityDimensions = dMap.entity
			}
		}
	}
	return nil
}

func (r *ProfileRepo) loadFieldPolicies(ctx context.Context, querier postgres.Querier, profile *security_profile.SecurityProfile) error {
	q, args, err := r.Builder().
		Select("entity_name", "action", "allowed_fields", "table_parts").
		From("security_profile_field_policies").
		Where(squirrel.Eq{"profile_id": profile.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build field policies query: %w", err)
	}

	rows, err := querier.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query field policies: %w", err)
	}
	defer rows.Close()

	policies := make(map[string]*security.FieldPolicy)
	for rows.Next() {
		var entityName, action string
		var allowedFields []string
		var tablePartsRaw []byte

		if err := rows.Scan(&entityName, &action, &allowedFields, &tablePartsRaw); err != nil {
			return fmt.Errorf("scan field policy: %w", err)
		}

		var tableParts map[string][]string
		if len(tablePartsRaw) > 0 && string(tablePartsRaw) != "{}" {
			if err := json.Unmarshal(tablePartsRaw, &tableParts); err != nil {
				return fmt.Errorf("unmarshal table_parts: %w", err)
			}
		}

		key := entityName + ":" + action
		policies[key] = &security.FieldPolicy{
			EntityName:    entityName,
			Action:        action,
			AllowedFields: allowedFields,
			TableParts:    tableParts,
		}
	}
	profile.FieldPolicies = policies
	return rows.Err()
}

func (r *ProfileRepo) loadFieldPoliciesBatch(ctx context.Context, querier postgres.Querier, profiles []*security_profile.SecurityProfile, profileIDs []id.ID) error {
	q, args, err := r.Builder().
		Select("profile_id", "entity_name", "action", "allowed_fields", "table_parts").
		From("security_profile_field_policies").
		Where(squirrel.Eq{"profile_id": profileIDs}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build field policies query: %w", err)
	}

	rows, err := querier.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("query field policies: %w", err)
	}
	defer rows.Close()

	policiesMap := make(map[id.ID]map[string]*security.FieldPolicy)
	for _, pid := range profileIDs {
		policiesMap[pid] = make(map[string]*security.FieldPolicy)
	}

	for rows.Next() {
		var profileID id.ID
		var entityName, action string
		var allowedFields []string
		var tablePartsRaw []byte

		if err := rows.Scan(&profileID, &entityName, &action, &allowedFields, &tablePartsRaw); err != nil {
			return fmt.Errorf("scan field policy: %w", err)
		}

		var tableParts map[string][]string
		if len(tablePartsRaw) > 0 && string(tablePartsRaw) != "{}" {
			if err := json.Unmarshal(tablePartsRaw, &tableParts); err != nil {
				return fmt.Errorf("unmarshal table_parts: %w", err)
			}
		}

		key := entityName + ":" + action
		if pMap, ok := policiesMap[profileID]; ok {
			pMap[key] = &security.FieldPolicy{
				EntityName:    entityName,
				Action:        action,
				AllowedFields: allowedFields,
				TableParts:    tableParts,
			}
		}
	}
	
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range profiles {
		if pMap, ok := policiesMap[p.ID]; ok {
			p.FieldPolicies = pMap
		}
	}
	return nil
}

func (r *ProfileRepo) saveDimensions(ctx context.Context, querier postgres.Querier, profile *security_profile.SecurityProfile) error {
	if len(profile.Dimensions) == 0 && len(profile.EntityDimensions) == 0 {
		return nil
	}
	
	q := r.Builder().Insert("security_profile_dimensions").
		Columns("profile_id", "dimension_name", "entity_name", "allowed_ids")

	hasValues := false

	// Save global dimensions (entity_name = '')
	for dimName, allowedIDs := range profile.Dimensions {
		q = q.Values(profile.ID, dimName, "", allowedIDs)
		hasValues = true
	}
	// Save per-entity dimensions
	for entityName, dims := range profile.EntityDimensions {
		for dimName, allowedIDs := range dims {
			q = q.Values(profile.ID, dimName, entityName, allowedIDs)
			hasValues = true
		}
	}

	if !hasValues {
		return nil
	}

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build dimension insert: %w", err)
	}
	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert dimensions: %w", err)
	}
	
	return nil
}

func (r *ProfileRepo) saveFieldPolicies(ctx context.Context, querier postgres.Querier, profile *security_profile.SecurityProfile) error {
	if len(profile.FieldPolicies) == 0 {
		return nil
	}

	q := r.Builder().Insert("security_profile_field_policies").
		Columns("profile_id", "entity_name", "action", "allowed_fields", "table_parts")

	for _, policy := range profile.FieldPolicies {
		tablePartsJSON, err := json.Marshal(policy.TableParts)
		if err != nil {
			return fmt.Errorf("marshal table_parts: %w", err)
		}
		q = q.Values(profile.ID, policy.EntityName, policy.Action, policy.AllowedFields, tablePartsJSON)
	}
	
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build field policy insert: %w", err)
	}
	if _, err := querier.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert field policies: %w", err)
	}
	
	return nil
}

func (r *ProfileRepo) loadPolicyRules(ctx context.Context, querier postgres.Querier, profile *security_profile.SecurityProfile) error {
	q, args, err := r.Builder().
		Select("id", "profile_id", "name", "description", "entity_name",
			"actions", "expression", "effect", "priority", "enabled",
			"created_at", "updated_at").
		From("security_policy_rules").
		Where(squirrel.Eq{"profile_id": profile.ID}).
		OrderBy("priority DESC", "created_at ASC").
		ToSql()
	if err != nil {
		return fmt.Errorf("build policy rules query: %w", err)
	}

	var rules []*security_profile.PolicyRule
	if err := pgxscan.Select(ctx, querier, &rules, q, args...); err != nil {
		return fmt.Errorf("query policy rules: %w", err)
	}
	profile.PolicyRules = rules
	return nil
}

func (r *ProfileRepo) loadPolicyRulesBatch(ctx context.Context, querier postgres.Querier, profiles []*security_profile.SecurityProfile, profileIDs []id.ID) error {
	q, args, err := r.Builder().
		Select("id", "profile_id", "name", "description", "entity_name",
			"actions", "expression", "effect", "priority", "enabled",
			"created_at", "updated_at").
		From("security_policy_rules").
		Where(squirrel.Eq{"profile_id": profileIDs}).
		OrderBy("priority DESC", "created_at ASC").
		ToSql()
	if err != nil {
		return fmt.Errorf("build policy rules query: %w", err)
	}

	var rules []*security_profile.PolicyRule
	if err := pgxscan.Select(ctx, querier, &rules, q, args...); err != nil {
		return fmt.Errorf("query policy rules: %w", err)
	}
	
	rulesMap := make(map[id.ID][]*security_profile.PolicyRule)
	for _, rule := range rules {
		rulesMap[rule.ProfileID] = append(rulesMap[rule.ProfileID], rule)
	}
	
	for _, p := range profiles {
		if rls, ok := rulesMap[p.ID]; ok {
			p.PolicyRules = rls
		} else {
			p.PolicyRules = []*security_profile.PolicyRule{}
		}
	}

	return nil
}

// loadUserCounts returns a map of profileID (string) → user count.
func (r *ProfileRepo) loadUserCounts(ctx context.Context, querier postgres.Querier) (map[string]int, error) {
	query := `SELECT profile_id, COUNT(*) FROM user_security_profiles GROUP BY profile_id`
	rows, err := querier.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query user counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var profileID id.ID
		var count int
		if err := rows.Scan(&profileID, &count); err != nil {
			return nil, fmt.Errorf("scan user count: %w", err)
		}
		counts[profileID.String()] = count
	}
	return counts, rows.Err()
}

// ListUsersByProfileID returns users assigned to a specific profile.
func (r *ProfileRepo) ListUsersByProfileID(ctx context.Context, profileID id.ID) ([]security_profile.ProfileUser, error) {
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT u.id, u.email, u.first_name, u.last_name, u.is_active
		FROM users u
		INNER JOIN user_security_profiles usp ON u.id = usp.user_id
		WHERE usp.profile_id = $1 AND u.deletion_mark = FALSE
		ORDER BY u.email ASC
	`

	rows, err := querier.Query(ctx, query, profileID)
	if err != nil {
		return nil, fmt.Errorf("query profile users: %w", err)
	}
	defer rows.Close()

	users := make([]security_profile.ProfileUser, 0, 16)
	for rows.Next() {
		var u security_profile.ProfileUser
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.IsActive); err != nil {
			return nil, fmt.Errorf("scan profile user: %w", err)
		}
		users = append(users, u)
	}

	return users, rows.Err()
}

// GetProfileBriefByUserIDs returns a map of userID → profile brief for batch enrichment.
func (r *ProfileRepo) GetProfileBriefByUserIDs(ctx context.Context, userIDs []id.ID) (map[id.ID]*security_profile.ProfileBrief, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	querier := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT usp.user_id, sp.id, sp.code, sp.name
		FROM user_security_profiles usp
		INNER JOIN security_profiles sp ON sp.id = usp.profile_id
		WHERE usp.user_id = ANY($1)
	`

	rows, err := querier.Query(ctx, query, userIDs)
	if err != nil {
		return nil, fmt.Errorf("query user profiles: %w", err)
	}
	defer rows.Close()

	result := make(map[id.ID]*security_profile.ProfileBrief)
	for rows.Next() {
		var userID, profileID id.ID
		var code, name string
		if err := rows.Scan(&userID, &profileID, &code, &name); err != nil {
			return nil, fmt.Errorf("scan user profile: %w", err)
		}
		result[userID] = &security_profile.ProfileBrief{
			ID:   profileID,
			Code: code,
			Name: name,
		}
	}

	return result, rows.Err()
}

// Compile-time check.
var _ security_profile.Repository = (*ProfileRepo)(nil)
