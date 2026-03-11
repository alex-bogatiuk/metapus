# ABAC Security — Implementation Tasks

## Phase 0: Data Scope Infrastructure
- [ ] Extend [UserContext](file:///c:/Users/user/go/src/metapus/internal/core/context/user.go#9-19) with `CounterpartyIDs` and security profile
- [ ] Create `internal/core/security/data_scope.go` — `DataScope` with `ApplyToSelect`, `CanAccessRecord`, `CanMutate`
- [ ] Add `DataScope` field to [ListFilter](file:///c:/Users/user/go/src/metapus/internal/domain/repository.go#17-45)
- [ ] Create DataScope context helpers (`NewDataScopeFromContext`)

## Phase 1: RLS Integration
- [ ] Integrate DataScope into `BaseCatalogRepo.buildWhereConditions`
- [ ] Integrate DataScope into document repo [buildWhereConditions](file:///c:/Users/user/go/src/metapus/internal/infrastructure/storage/postgres/catalog_repo/base.go#209-248)
- [ ] Add RLS checks in `CatalogService.GetByID` (post-fetch)
- [ ] Add RLS checks in `BaseDocumentService.GetByID/Update/Post/Unpost/Delete`
- [ ] Add RLS write-check for "org transfer" scenario (2.3)
- [ ] Inject DataScope in service layer List methods

## Phase 2: FLS — Field-Level Security
- [ ] Create `internal/core/security/field_policy.go` — `FieldPolicy` + `IsFieldAllowed`
- [ ] Create `internal/core/security/field_masker.go` — cached reflector for read/write masking
- [ ] Integrate FLS validation in document Update handler
- [ ] Integrate FLS masking in document Get handler

## Phase 3: Tests
- [ ] Unit tests for `DataScope` (ApplyToSelect, CanAccessRecord)
- [ ] Unit tests for `FieldPolicy` (IsFieldAllowed, wildcards, exclusions)
- [ ] Unit tests for `FieldMasker` (MaskForRead, ValidateWrite)
- [ ] Build verification (`go build ./...`)
