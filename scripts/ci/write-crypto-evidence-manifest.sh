#!/usr/bin/env bash
set -euo pipefail

output_path="${1:?output path is required}"
scenario_scope="${2:?scenario scope is required}"
evidence_mode="${3:-index}"
if [[ "$evidence_mode" != "verified" && "$evidence_mode" != "index" ]]; then
  echo "evidence mode must be verified or index" >&2
  exit 2
fi
mkdir -p "$(dirname "$output_path")"
mkdir -p crypto-evidence frontend/test-results frontend/playwright-report

migration_digest="$(find db/migrations -maxdepth 1 -type f -name '*.sql' -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)"
config_digest="$(printf '%s\n' "${GO_VERSION:-}" "${NODE_VERSION:-}" "${RUNNER_OS:-}" | sha256sum | cut -d' ' -f1)"
conclusion="${JOB_STATUS:-${GITHUB_JOB_STATUS:-unknown}}"
verdict="NOT_RUN"
if [[ "$evidence_mode" == "verified" ]]; then
  case "$conclusion" in
    success) verdict="PASS" ;;
    failure) verdict="FAIL" ;;
    cancelled) verdict="BLOCKED" ;;
  esac
fi
junit_files="$(find crypto-evidence frontend/test-results -type f \( -name '*junit*.xml' -o -name '*-junit.xml' \) 2>/dev/null | sort | jq -R . | jq -s .)"
playwright_files="$(find frontend/playwright-report frontend/test-results -type f 2>/dev/null | sort | jq -R . | jq -s .)"
financial_snapshot_files="$(find crypto-evidence -type f \( -name '*financial*.json' -o -name '*snapshot*.json' \) ! -name '*manifest*.json' 2>/dev/null | sort | jq -R . | jq -s .)"

jq -n \
  --arg commit "${GITHUB_SHA:-$(git rev-parse HEAD)}" \
  --arg runId "${GITHUB_RUN_ID:-local}" \
  --arg runAttempt "${GITHUB_RUN_ATTEMPT:-1}" \
  --arg generatedAt "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
  --arg scenarioScope "$scenario_scope" \
  --arg evidenceMode "$evidence_mode" \
  --arg conclusion "$conclusion" \
  --arg verdict "$verdict" \
  --arg testCommand "${TEST_COMMAND:-unspecified}" \
  --arg migrationDigest "$migration_digest" \
  --arg configDigest "$config_digest" \
  --argjson junitFiles "$junit_files" \
  --argjson playwrightFiles "$playwright_files" \
  --argjson financialSnapshotFiles "$financial_snapshot_files" \
  '{
    schemaVersion: 61,
    commit: $commit,
    runId: $runId,
    runAttempt: $runAttempt,
    generatedAt: $generatedAt,
    scenarioScope: $scenarioScope,
    evidenceMode: $evidenceMode,
    conclusion: $conclusion,
    verdict: $verdict,
    testCommand: $testCommand,
    migrationDigest: $migrationDigest,
    configDigest: $configDigest,
    evidence: {
      junit: $junitFiles,
      playwright: $playwrightFiles,
      financialSnapshots: $financialSnapshotFiles
    }
  }' > "$output_path"
