#!/usr/bin/env bash

# Sourced by test-installed-release.sh after its fixture helpers are defined.

declare output release

fixture_pass() {
  local target="$1" scenario="$2" phase="$3" review_attempt="$4"
  shift 4
  local started=${SECONDS}
  expect_pass "${target}" "${scenario} ${phase}" "$@"
  printf 'FIXTURE METRIC scenario=%s phase=%s duration_seconds=%d review_attempt=%s\n' \
    "${scenario}" "${phase}" "$((SECONDS - started))" "${review_attempt}"
}

fixture_expect_rejection() {
  local target="$1" scenario="$2" phase="$3" review_attempt="$4" expected_status="$5"
  shift 5
  local started=${SECONDS} status
  if run_policy "${target}" "$@"; then
    fail "${scenario} ${phase}: ${*} reported nothing: $(excerpt)"
  else
    status=$?
  fi
  if [[ "${status}" != "${expected_status}" ]]; then
    fail "${scenario} ${phase}: ${*} exited ${status}, want ${expected_status}: $(excerpt)"
  fi
  printf 'FIXTURE METRIC scenario=%s phase=%s duration_seconds=%d review_attempt=%s outcome=rejected exit_status=%s\n' \
    "${scenario}" "${phase}" "$((SECONDS - started))" "${review_attempt}" "${status}"
}

assert_behavior_review_status_line() {
  local scenario="$1" expected="$2" count
  count="$(grep -Fc 'BEHAVIOR REVIEW:' "${output}" || true)"
  if [[ "${count}" != 1 ]] || ! grep -Fxq "BEHAVIOR REVIEW: ${expected}" "${output}"; then
    fail "${scenario}: expected exactly one behavior-review status ${expected}: $(excerpt)"
  fi
  assert_final_state_status_line "${scenario}" "${expected}"
}

assert_final_state_status_line() {
  local scenario="$1" behavior="$2" expected count
  case "${behavior}" in
    "NOT RUN (optional)") expected="NOT RUN (optional)" ;;
    REQUIRED*) expected="NOT RUN (required)" ;;
    PASSED*) expected="PASSED${behavior#PASSED}" ;;
    FAILED*) expected="FAILED${behavior#FAILED}" ;;
    *) fail "${scenario}: cannot map behavior status ${behavior} to final-state status" ;;
  esac
  count="$(grep -Fc 'FINAL STATE:' "${output}" || true)"
  if [[ "${count}" != 1 ]] || ! grep -Fxq "FINAL STATE: ${expected}" "${output}"; then
    fail "${scenario}: expected exactly one final-state status ${expected}: $(excerpt)"
  fi
}

fixture_status() {
  local target="$1" scenario="$2" base="$3" expected="$4"
  fixture_pass "${target}" "${scenario}" status 0 behavior-review status --base "${base}"
  assert_behavior_review_status_line "${scenario}: status" "${expected}"
}

fixture_gate_pass() {
  local target="$1" scenario="$2" phase="$3" base="$4" expected="$5" review_attempt="$6" command="$7"
  local fixture_python
  fixture_python="$(installed_fixture_python "${release}")"
  fixture_accept_architecture "${target}" "${base}" "${fixture_python}"
  fixture_pass "${target}" "${scenario}" "${phase}" "${review_attempt}" "${command}" --base "${base}"
  assert_behavior_review_status_line "${scenario}: ${phase}" "${expected}"
}

fixture_gate_requires_review() {
  local target="$1" scenario="$2" phase="$3" base="$4" expected="$5" command="$6"
  local command_log="${target}/.git/code-polishy-command-log"
  : >"${command_log}"
  fixture_expect_rejection "${target}" "${scenario}" "${phase}" 0 1 "${command}" --base "${base}"
  assert_behavior_review_status_line "${scenario}: ${phase}" "${expected}"
  expect_finding "${scenario} ${phase}" "policy.behaviorReview" \
    ".code-polishy-reports/behavior-review/receipt.json" "gate-receipt"
  expect_no_target_commands "${target}" "${scenario}: ${phase} ran ordinary commands before required review"
}

fixture_gate_reports_stale_review() {
  local target="$1" scenario="$2" base="$3"
  local command_log="${target}/.git/code-polishy-command-log"
  : >"${command_log}"
  fixture_expect_rejection "${target}" "${scenario}" stale-review 1 1 merge-gate --base "${base}"
  assert_behavior_review_status_line "${scenario}: stale review" 'FAILED (checkout)'
  expect_finding "${scenario} stale review" "policy.behaviorReview" \
    ".code-polishy-reports/behavior-review/receipt.json" "gate-receipt"
  expect_no_target_commands "${target}" "${scenario}: stale review ran ordinary commands"
}

fixture_summary() {
  local target="$1" scenario="$2" review_attempts="$3"
  local artifact_root="${target}/.code-polishy-reports/behavior-review" artifact_bytes=0
  if [[ -d "${artifact_root}" ]]; then
    artifact_bytes="$(find "${artifact_root}" -type f -exec wc -c {} + | awk 'END { print $1 }')"
  fi
  if [[ ! "${artifact_bytes}" =~ ^[0-9]+$ ]]; then
    fail "${scenario}: behavior-review artifact size was unavailable"
  fi
  printf 'FIXTURE METRIC scenario=%s artifact_bytes=%s review_attempts=%s\n' \
    "${scenario}" "${artifact_bytes}" "${review_attempts}"
}

write_behavior_review_fixture_config() {
  local target="$1" behavior_policy="$2"
  local behavior_review=""
  if [[ -n "${behavior_policy}" ]]; then
    behavior_review=$',\n    "behaviorReview": '"${behavior_policy}"
  fi
  write_file "${target}/.code-polishy.json" <<EOF
{
  "version": 4,
  "project": { "kind": "application", "capabilities": [] },
  "scope": {},
  "quality": {},
  "modules": [
    { "name": "greeting", "paths": ["internal/greeting/**"] },
    { "name": "tooling", "paths": ["scripts/**"] }
  ],
  "checks": [
    {
      "name": "fixture-build",
      "provides": ["build"],
      "argv": ["./scripts/build.sh"],
      "modules": ["greeting", "tooling"],
      "runOn": ["build"]
    }
  ],
  "tests": {
    "ownership": [
      {"paths": ["internal/greeting/greeting_test.go"], "module": "greeting", "focusedSuite": "greeting-contract"}
    ],
    "suites": [
      {
        "name": "greeting-unit",
        "kind": "unit",
        "scope": "module",
        "modules": ["greeting"],
        "argv": ["./scripts/test.sh", "greeting-unit"],
        "runOn": ["full"]
      },
      {
        "name": "greeting-contract",
        "kind": "unit",
        "scope": "module",
        "modules": ["greeting"],
        "paths": ["internal/greeting/greeting_test.go"],
        "argv": ["./scripts/test.sh", "greeting-contract"],
        "runOn": ["focused", "recommended", "full"]
      },
      {
        "name": "tooling-contract",
        "kind": "unit",
        "scope": "module",
        "modules": ["tooling"],
        "argv": ["./scripts/test.sh", "tooling-contract"],
        "runOn": ["focused", "recommended", "full"]
      },
      {
        "name": "repository-full",
        "kind": "integration",
        "scope": "repository",
        "cost": "standard",
        "argv": ["./scripts/test.sh", "repository-full"],
        "runOn": ["recommended", "full"]
      }
    ]
  },
  "verification": {
    "mergeGate": {"recommendedModules": ["greeting"]}${behavior_review}
  },
  "exceptions": []
}
EOF
}

write_behavior_review_commands() {
  local target="$1"
  write_file "${target}/scripts/build.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command_log="$(git rev-parse --path-format=absolute --git-common-dir)/code-polishy-command-log"
printf '%s\n' "$(basename "$0")" >>"${command_log}"
EOF
  write_file "${target}/scripts/test.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
command_log="$(git rev-parse --path-format=absolute --git-common-dir)/code-polishy-command-log"
printf '%s %s\n' "$(basename "$0")" "$*" >>"${command_log}"
go test ./...
EOF
  chmod +x "${target}/scripts/build.sh" "${target}/scripts/test.sh"
}

new_behavior_review_target() {
  local target="$1" behavior_policy="$2" git_executable="$3"
  mkdir -p "${target}"
  write_file "${target}/go.mod" <<'EOF'
module example.test/installed-behavior-review

go 1.26
EOF
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

func Render(name string) string {
	return "hello, " + name
}
EOF
  write_file "${target}/README.md" <<'EOF'
# Installed behavior-review fixture
EOF
  write_target_commands "${target}"
  write_behavior_review_commands "${target}"
  write_security_workflow "${target}"
  write_behavior_review_fixture_config "${target}" "${behavior_policy}"
  seal_target "${target}"
  "${git_executable}" -C "${target}" branch -M main
  "${git_executable}" -C "${target}" switch --quiet -c feature
}

commit_requested_greeting() {
  local target="$1" git_executable="$2" message="$3"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

func Render(name string) string {
	return "hi, " + name
}
EOF
  write_file "${target}/internal/greeting/greeting_test.go" <<'EOF'
package greeting

import "testing"

func TestRenderNamesTheGreeted(t *testing.T) {
	if got := Render("world"); got != "hi, world" {
		t.Fatalf("Render() = %q", got)
	}
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go internal/greeting/greeting_test.go
  "${git_executable}" -C "${target}" commit --quiet -m "${message}"
}

commit_refactored_greeting() {
  local target="$1" git_executable="$2" message="$3"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

const greetingPrefix = "hi, "

func Render(name string) string {
	return greetingPrefix + name
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go
  "${git_executable}" -C "${target}" commit --quiet -m "${message}"
}

stage_correction_residue_greeting() {
  local target="$1" git_executable="$2"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

var rejectedBroccoliPrefix = ""

func Render(name string) string {
	if rejectedBroccoliPrefix != "" {
		return rejectedBroccoliPrefix + name
	}
	return "hi, " + name
}
EOF
  write_file "${target}/internal/greeting/greeting_test.go" <<'EOF'
package greeting

import "testing"

func TestRenderNamesTheGreeted(t *testing.T) {
	if got := Render("world"); got != "hi, world" {
		t.Fatalf("Render() = %q", got)
	}
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go internal/greeting/greeting_test.go
}

commit_broken_greeting() {
  local target="$1" git_executable="$2"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

func Render(name string) string {
	return "broken, " + name
}
EOF
  write_file "${target}/internal/greeting/greeting_test.go" <<'EOF'
package greeting

import "testing"

func TestRenderNamesTheGreeted(t *testing.T) {
	if got := Render("world"); got != "broken, world" {
		t.Fatalf("Render() = %q", got)
	}
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go internal/greeting/greeting_test.go
  "${git_executable}" -C "${target}" commit --quiet -m "unintended greeting behavior"
}

commit_fixed_greeting() {
  local target="$1" git_executable="$2"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

const greetingPrefix = "hi, "

func Render(name string) string {
	return greetingPrefix + name
}
EOF
  write_file "${target}/internal/greeting/greeting_test.go" <<'EOF'
package greeting

import "testing"

func TestRenderNamesTheGreeted(t *testing.T) {
	if got := Render("world"); got != "hi, world" {
		t.Fatalf("Render() = %q", got)
	}
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go internal/greeting/greeting_test.go
  "${git_executable}" -C "${target}" commit --quiet -m "restore greeting contract"
}

capture_fixture_intent() {
  local target="$1" scenario="$2" phase="$3" scratch_root="$4"
  shift 4
  local intent="${scratch_root}/${scenario}-${phase}.txt"
  printf 'Fixture request for %s during %s.\n' "${scenario}" "${phase}" >"${intent}"
  local command=(behavior-review capture-intent --intent-file "${intent}")
  local feature
  for feature in "$@"; do
    command+=(--feature "${feature}")
  done
  fixture_pass "${target}" "${scenario}" "${phase}" 0 "${command[@]}"
}

assert_task_features() {
  local target="$1"
  shift
  local journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  [[ -f "${journal}" ]] || fail "$(basename "${target}"): capture did not create its intent journal"
  local feature
  for feature in "$@"; do
    grep -Fq "\"${feature}\"" "${journal}" ||
      fail "$(basename "${target}"): journal does not bind feature ${feature}"
  done
}

assert_intent_capture_count() {
  local target="$1" want="$2"
  local journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json" count
  [[ -f "${journal}" ]] || fail "$(basename "${target}"): capture did not create its intent journal"
  count="$(grep -c '"id": "intent-' "${journal}" || true)"
  if [[ "${count}" != "${want}" ]]; then
    fail "$(basename "${target}"): intent capture count ${count}, want ${want}"
  fi
}

assert_task_requirement_count() {
  local target="$1" want="$2"
  local journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  local count
  count="$(grep -c '"id": "requirement-' "${journal}" || true)"
  if [[ "${count}" != "${want}" ]]; then
    fail "$(basename "${target}"): requirement record count ${count}, want ${want}"
  fi
}

assert_no_behavior_review_artifacts() {
  local target="$1" scenario="$2"
  if [[ -e "${target}/.code-polishy-reports/behavior-review" ]]; then
    fail "${scenario}: optional behavior-review probe created AI artifacts"
  fi
}

assert_no_review_packet_or_receipt() {
  local target="$1" scenario="$2"
  local artifact_root="${target}/.code-polishy-reports/behavior-review"
  local artifact
  for artifact in packet.json receipt.json; do
    if [[ -e "${artifact_root}/${artifact}" ]]; then
      fail "${scenario}: optional behavior review created ${artifact}"
    fi
  done
}

assert_packet_v4() {
  local target="$1" full_candidate="$2"
  shift 2
  local packet="${target}/.code-polishy-reports/behavior-review/packet.json"
  local review_id selection_sha requirement_sha decision_sha
  [[ -f "${packet}" ]] || fail "$(basename "${target}"): behavior-review packet is missing"
  grep -Fq '"version": 4' "${packet}" || fail "$(basename "${target}"): packet is not schema v4"
  review_id="$(lock_field "${packet}" review_id)"
  selection_sha="$(lock_field "${packet}" selection_sha256)"
  requirement_sha="$(lock_field "${packet}" requirement_sha256)"
  decision_sha="$(lock_field "${packet}" decision_sha256)"
  if [[ ! "${review_id}" =~ ^review-[0-9a-f]{32}$ ||
    ! "${selection_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${requirement_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${decision_sha}" =~ ^[0-9a-f]{64}$ ]]; then
    fail "$(basename "${target}"): packet lacks valid v4 review bindings"
  fi
  if [[ "${full_candidate}" == true ]]; then
    grep -Fq '"full_candidate": true' "${packet}" ||
      fail "$(basename "${target}"): strict review packet lacks full-candidate scope"
  else
    grep -Fq '"full_candidate": false' "${packet}" ||
      fail "$(basename "${target}"): feature packet unexpectedly has full-candidate scope"
  fi
  local feature
  for feature in "$@"; do
    grep -Fq "\"name\": \"${feature}\"" "${packet}" ||
      fail "$(basename "${target}"): packet lacks selected feature ${feature}"
  done
}

assert_receipt_v4() {
  local target="$1"
  local receipt="${target}/.code-polishy-reports/behavior-review/receipt.json"
  local selection_sha requirement_sha decision_sha
  [[ -f "${receipt}" ]] || fail "$(basename "${target}"): behavior-review receipt is missing"
  grep -Fq '"version": 4' "${receipt}" || fail "$(basename "${target}"): receipt is not schema v4"
  selection_sha="$(lock_field "${receipt}" selection_sha256)"
  requirement_sha="$(lock_field "${receipt}" requirement_sha256)"
  decision_sha="$(lock_field "${receipt}" decision_sha256)"
  if [[ ! "${selection_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${requirement_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${decision_sha}" =~ ^[0-9a-f]{64}$ ]]; then
    fail "$(basename "${target}"): receipt lacks valid v4 review bindings"
  fi
}

write_single_review_result() {
  local target="$1" classification="$2" proof_ids="$3" scope_features="$4" full_candidate="$5" before="$6" after="$7"
  local packet="${target}/.code-polishy-reports/behavior-review/packet.json"
  local result="${target}/.code-polishy-reports/behavior-review/result.json"
  local review_id base candidate intent_sha selection_sha decision_sha
  review_id="$(lock_field "${packet}" review_id)"
  base="$(lock_field "${packet}" base)"
  candidate="$(lock_field "${packet}" candidate)"
  intent_sha="$(lock_field "${packet}" intent_sha256)"
  selection_sha="$(lock_field "${packet}" selection_sha256)"
  decision_sha="$(lock_field "${packet}" decision_sha256)"
  if [[ ! "${review_id}" =~ ^review-[0-9a-f]{32}$ ||
    ! "${base}" =~ ^[0-9a-f]{40,64}$ ||
    ! "${candidate}" =~ ^[0-9a-f]{40,64}$ ||
    ! "${intent_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${selection_sha}" =~ ^[0-9a-f]{64}$ ||
    ! "${decision_sha}" =~ ^[0-9a-f]{64}$ ]]; then
    fail "installed behavior review packet has invalid v4 result bindings"
  fi
  write_file "${result}" <<EOF
{
  "version": 4,
  "review_id": "${review_id}",
  "base": "${base}",
  "candidate": "${candidate}",
  "intent_sha256": "${intent_sha}",
  "selection_sha256": "${selection_sha}",
  "decision_sha256": "${decision_sha}",
  "behaviors": [
    {
      "before": "${before}",
      "after": "${after}",
      "classification": "${classification}",
      "proof_ids": ${proof_ids},
      "scope": {
        "features": ${scope_features},
        "full_candidate": ${full_candidate}
      }
    }
  ],
  "findings": [],
  "final_state_findings": []
}
EOF
}

write_requested_review_result() {
  write_single_review_result "$1" requested "[\"$2\"]" "$3" "$4" \
    "Render returns the old greeting." "Render returns the requested new greeting."
}

write_preserved_review_result() {
  write_single_review_result "$1" preserved '[]' "$2" "$3" \
    "Render returns the same greeting." "Render preserves the greeting while changing its implementation."
}

write_final_state_finding_result() {
  local target="$1" path="$2" kind="$3" summary="$4"
  local packet="${target}/.code-polishy-reports/behavior-review/packet.json"
  local journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  local result="${target}/.code-polishy-reports/behavior-review/result.json"
  local review_id base candidate intent_sha selection_sha decision_sha hunk_sha line intent_id
  review_id="$(lock_field "${packet}" review_id)"
  base="$(lock_field "${packet}" base)"
  candidate="$(lock_field "${packet}" candidate)"
  intent_sha="$(lock_field "${packet}" intent_sha256)"
  selection_sha="$(lock_field "${packet}" selection_sha256)"
  decision_sha="$(lock_field "${packet}" decision_sha256)"
  hunk_sha="$(awk -v path="${path}" '
    $0 ~ "\"path\": \"" path "\"" { selected = 1; next }
    selected && $0 ~ /"sha256": "/ {
      value = $0
      sub(/^.*"sha256": "/, "", value)
      sub(/".*$/, "", value)
      print value
      exit
    }
  ' "${packet}")"
  line="$(awk -v path="${path}" '
    $0 ~ "\"path\": \"" path "\"" { selected = 1; next }
    selected && $0 ~ /"candidate_start": [0-9]+/ {
      value = $0
      sub(/^.*"candidate_start": /, "", value)
      sub(/,.*/, "", value)
      print value
      exit
    }
  ' "${packet}")"
  intent_id="$(awk '/"id": "intent-/ {
    value = $0
    sub(/^.*"id": "/, "", value)
    sub(/".*$/, "", value)
    found = value
  }
  END { print found }
  ' "${journal}")"
  if [[ ! "${hunk_sha}" =~ ^[0-9a-f]{64}$ || ! "${line}" =~ ^[1-9][0-9]*$ || ! "${intent_id}" =~ ^intent-[0-9a-f]{32}$ ]]; then
    fail "installed final-state evidence bindings are invalid"
  fi
  write_file "${result}" <<EOF
{
  "version": 4,
  "review_id": "${review_id}",
  "base": "${base}",
  "candidate": "${candidate}",
  "intent_sha256": "${intent_sha}",
  "selection_sha256": "${selection_sha}",
  "decision_sha256": "${decision_sha}",
  "behaviors": [
    {
      "before": "The greeting remains available.",
      "after": "The greeting remains available.",
      "classification": "preserved",
      "proof_ids": [],
      "scope": {"features": [], "full_candidate": true}
    }
  ],
  "findings": [],
  "final_state_findings": [
    {
      "kind": "${kind}",
      "path": "${path}",
      "line": ${line},
      "patch_hunk_sha256": "${hunk_sha}",
      "intent_ids": ["${intent_id}"],
      "summary": "${summary}"
    }
  ]
}
EOF
}

record_requested_behavior_review() {
  local target="$1" scenario="$2" base="$3" proof_id="$4" suite="$5" scope_features="$6" full_candidate="$7"
  shift 7
  fixture_pass "${target}" "${scenario}" prepare 1 behavior-review prepare --base "${base}"
  assert_packet_v4 "${target}" "${full_candidate}" "$@"
  fixture_pass "${target}" "${scenario}" regression-proof 1 \
    regression-proof --base "${base}" --suite "${suite}" \
    --evidence internal/greeting/greeting_test.go --id "${proof_id}"
  write_requested_review_result "${target}" "${proof_id}" "${scope_features}" "${full_candidate}"
  fixture_pass "${target}" "${scenario}" finalize 1 behavior-review finalize --base "${base}"
  assert_receipt_v4 "${target}"
}

record_preserved_behavior_review() {
  local target="$1" scenario="$2" base="$3" scope_features="$4" full_candidate="$5"
  shift 5
  fixture_pass "${target}" "${scenario}" reprepare 2 behavior-review prepare --base "${base}"
  assert_packet_v4 "${target}" "${full_candidate}" "$@"
  write_preserved_review_result "${target}" "${scope_features}" "${full_candidate}"
  fixture_pass "${target}" "${scenario}" refinalize 2 behavior-review finalize --base "${base}"
  assert_receipt_v4 "${target}"
}

assert_feature_proof_suite_restriction() {
  local target="$1" scenario="$2" base="$3"
  fixture_expect_rejection "${target}" "${scenario}" feature-suite-rejected 1 2 \
    regression-proof --base "${base}" --suite greeting-contract \
    --evidence internal/greeting/greeting_test.go --id feature-suite-rejected
  grep -Fq 'is not eligible for the selected behavior review features' "${output}" ||
    fail "${scenario}: feature scope did not reject an unrelated ordinary suite: $(excerpt)"
}

assert_replay_and_suite_deduplication() {
  local target="$1" scenario="$2"
  local command_log="${target}/.git/code-polishy-command-log"
  local greeting_unit_runs
  greeting_unit_runs="$(grep -Fc 'test.sh greeting-unit' "${command_log}" || true)"
  if [[ "${greeting_unit_runs}" != 3 ]]; then
    fail "${scenario}: expected two proof replays plus one deduplicated forced greeting suite, got ${greeting_unit_runs}"
  fi
  printf 'FIXTURE METRIC scenario=%s greeting_unit_runs=%s proof_replay_runs=2 forced_suite_runs=1\n' \
    "${scenario}" "${greeting_unit_runs}"
}

exercise_no_config_behavior_review() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-no-config" target="${scratch_root}/behavior-no-config"
  new_behavior_review_target "${target}" "" "${git_executable}"
  local base command_log
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  command_log="${target}/.git/code-polishy-command-log"
  commit_requested_greeting "${target}" "${git_executable}" "optional greeting candidate"
  assert_no_behavior_review_artifacts "${target}" "${scenario} before status"
  fixture_status "${target}" "${scenario}" "${base}" "NOT RUN (optional)"
  assert_no_behavior_review_artifacts "${target}" "${scenario} after status"
  : >"${command_log}"
  fixture_gate_pass "${target}" "${scenario}" checkpoint "${base}" "NOT RUN (optional)" 0 checkpoint-gate
  [[ -s "${command_log}" ]] || fail "${scenario}: optional checkpoint skipped its ordinary commands"
  : >"${command_log}"
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "NOT RUN (optional)" 0 merge-gate
  [[ -s "${command_log}" ]] || fail "${scenario}: optional merge skipped its ordinary commands"
  assert_no_behavior_review_artifacts "${target}" "${scenario} after gates"
  fixture_summary "${target}" "${scenario}" 0
}

exercise_capture_time_features_and_replay() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-capture-time" target="${scratch_root}/behavior-capture-time"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"]},{"name":"search","description":"Search query and result behavior.","modules":["greeting"],"suites":["greeting-unit"]}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base command_log
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" capture "${scratch_root}" checkout search
  assert_task_features "${target}" checkout search
  commit_requested_greeting "${target}" "${git_executable}" "capture-time requested greeting"
  fixture_status "${target}" "${scenario}" "${base}" "REQUIRED (checkout, search)"
  fixture_pass "${target}" "${scenario}" prepare-feature-scope 1 behavior-review prepare --base "${base}"
  assert_packet_v4 "${target}" false checkout search
  assert_feature_proof_suite_restriction "${target}" "${scenario}" "${base}"
  record_requested_behavior_review "${target}" "${scenario}" "${base}" capture-time-proof greeting-unit '["checkout","search"]' false checkout search
  command_log="${target}/.git/code-polishy-command-log"
  : >"${command_log}"
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (checkout, search)" 1 merge-gate
  assert_replay_and_suite_deduplication "${target}" "${scenario}"
  fixture_summary "${target}" "${scenario}" 1
}

exercise_later_additive_require() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-later-require" target="${scratch_root}/behavior-later-require"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"]},{"name":"search","description":"Search query and result behavior.","modules":["greeting"],"suites":["greeting-unit"]}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base journal before after
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" original-request "${scratch_root}"
  assert_intent_capture_count "${target}" 1
  assert_task_requirement_count "${target}" 0
  commit_requested_greeting "${target}" "${git_executable}" "later feature request candidate"
  fixture_status "${target}" "${scenario}" "${base}" "NOT RUN (optional)"
  assert_no_review_packet_or_receipt "${target}" "${scenario} before feature request"
  fixture_pass "${target}" "${scenario}" require-checkout 1 behavior-review require --base "${base}" --feature checkout
  fixture_pass "${target}" "${scenario}" require-search 1 behavior-review require --base "${base}" --feature search
  assert_task_features "${target}" checkout search
  assert_task_requirement_count "${target}" 2
  fixture_status "${target}" "${scenario}" "${base}" "REQUIRED (checkout, search)"
  journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  before="$(cksum <"${journal}")"
  fixture_status "${target}" "${scenario}" "${base}" "REQUIRED (checkout, search)"
  after="$(cksum <"${journal}")"
  [[ "${before}" == "${after}" ]] || fail "${scenario}: status changed the additive requirement journal"
  record_requested_behavior_review "${target}" "${scenario}" "${base}" later-require-proof greeting-unit '["checkout","search"]' false checkout search
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (checkout, search)" 1 merge-gate
  fixture_summary "${target}" "${scenario}" 1
}

exercise_merge_required_feature() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-merge-required" target="${scratch_root}/behavior-merge-required"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"],"requiredAt":"merge"}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" capture "${scratch_root}"
  commit_requested_greeting "${target}" "${git_executable}" "merge-required greeting"
  fixture_gate_pass "${target}" "${scenario}" checkpoint "${base}" "NOT RUN (optional)" 0 checkpoint-gate
  fixture_gate_requires_review "${target}" "${scenario}" merge-missing-receipt "${base}" "REQUIRED (checkout)" merge-gate
  record_requested_behavior_review "${target}" "${scenario}" "${base}" merge-required-proof greeting-unit '["checkout"]' false checkout
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (checkout)" 1 merge-gate
  fixture_summary "${target}" "${scenario}" 1
}

exercise_checkpoint_required_feature() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-checkpoint-required" target="${scratch_root}/behavior-checkpoint-required"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"],"requiredAt":"checkpoint"}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" capture "${scratch_root}"
  commit_requested_greeting "${target}" "${git_executable}" "checkpoint-required greeting"
  fixture_gate_requires_review "${target}" "${scenario}" checkpoint-missing-receipt "${base}" "REQUIRED (checkout)" checkpoint-gate
  record_requested_behavior_review "${target}" "${scenario}" "${base}" checkpoint-required-proof greeting-unit '["checkout"]' false checkout
  fixture_gate_pass "${target}" "${scenario}" checkpoint "${base}" "PASSED (checkout)" 1 checkpoint-gate
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (checkout)" 1 merge-gate
  fixture_summary "${target}" "${scenario}" 1
}

exercise_strict_full_candidate() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-strict-full-candidate" target="${scratch_root}/behavior-strict-full-candidate"
  local policy='{"defaultRequiredAt":"merge","features":[]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" capture "${scratch_root}"
  commit_requested_greeting "${target}" "${git_executable}" "strict greeting candidate"
  fixture_gate_requires_review "${target}" "${scenario}" merge-missing-receipt "${base}" "REQUIRED (all changes)" merge-gate
  record_requested_behavior_review "${target}" "${scenario}" "${base}" strict-proof greeting-contract '[]' true
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (all changes)" 1 merge-gate
  fixture_summary "${target}" "${scenario}" 1
}

exercise_stale_fix_and_rereview() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-stale-fix-rereview" target="${scratch_root}/behavior-stale-fix-rereview"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"],"requiredAt":"merge"}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base repair_base started
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" first-capture "${scratch_root}"
  commit_requested_greeting "${target}" "${git_executable}" "first reviewed greeting"
  record_requested_behavior_review "${target}" "${scenario}" "${base}" first-proof greeting-unit '["checkout"]' false checkout
  fixture_gate_pass "${target}" "${scenario}" first-merge "${base}" "PASSED (checkout)" 1 merge-gate

  repair_base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" repair-capture "${scratch_root}"
  commit_broken_greeting "${target}" "${git_executable}"
  fixture_pass "${target}" "${scenario}" prepare-unintended 1 behavior-review prepare --base "${repair_base}"
  assert_packet_v4 "${target}" false checkout
  write_single_review_result "${target}" unintended '[]' '["checkout"]' false \
    "Render returns the expected greeting." "Render returns an unintended broken greeting."
  started=${SECONDS}
  if run_policy "${target}" behavior-review finalize --base "${repair_base}"; then
    fail "${scenario}: unintended review finalized successfully"
  fi
  grep -Fq 'reviewer classified a behavior as unintended' "${output}" ||
    fail "${scenario}: unintended review did not explain its block: $(excerpt)"
  printf 'FIXTURE METRIC scenario=%s phase=unintended-block duration_seconds=%d review_attempt=1 outcome=blocked\n' \
    "${scenario}" "$((SECONDS - started))"

  commit_fixed_greeting "${target}" "${git_executable}"
  fixture_gate_reports_stale_review "${target}" "${scenario}" "${repair_base}"
  record_preserved_behavior_review "${target}" "${scenario}" "${repair_base}" '["checkout"]' false checkout
  fixture_gate_pass "${target}" "${scenario}" repaired-merge "${repair_base}" "PASSED (checkout)" 2 merge-gate
  fixture_summary "${target}" "${scenario}" 2
}

exercise_multi_task_union() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-multi-task-union" target="${scratch_root}/behavior-multi-task-union"
  local policy='{"defaultRequiredAt":"on-request","features":[{"name":"checkout","description":"Checkout completion and payment behavior.","modules":["greeting"],"suites":["greeting-unit"]},{"name":"search","description":"Search query and result behavior.","modules":["greeting"],"suites":["greeting-unit"]}]}'
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  local base second_base journal
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" first-task "${scratch_root}" checkout
  commit_requested_greeting "${target}" "${git_executable}" "first task greeting"
  second_base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" second-task "${scratch_root}" search
  commit_refactored_greeting "${target}" "${git_executable}" "second task greeting refactor"
  assert_intent_capture_count "${target}" 2
  assert_task_features "${target}" checkout search
  assert_task_requirement_count "${target}" 2
  journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  grep -Fq "\"task_base\": \"${base}\"" "${journal}" ||
    fail "${scenario}: first feature is not bound to the first task base"
  grep -Fq "\"task_base\": \"${second_base}\"" "${journal}" ||
    fail "${scenario}: second feature is not bound to the later task base"
  fixture_status "${target}" "${scenario}" "${base}" "REQUIRED (checkout, search)"
  record_requested_behavior_review "${target}" "${scenario}" "${base}" multi-task-proof greeting-unit '["checkout","search"]' false checkout search
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (checkout, search)" 1 merge-gate
  fixture_summary "${target}" "${scenario}" 1
}

exercise_dirty_correction_and_final_state() {
  local scratch_root="$1" git_executable="$2"
  local scenario="behavior-final-state" target="${scratch_root}/behavior-final-state"
  local policy='{"defaultRequiredAt":"checkpoint","features":[]}'
  local base journal
  new_behavior_review_target "${target}" "${policy}" "${git_executable}"
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  capture_fixture_intent "${target}" "${scenario}" original "${scratch_root}"
  stage_correction_residue_greeting "${target}" "${git_executable}"
  capture_fixture_intent "${target}" "${scenario}" remove-broccoli "${scratch_root}"
  journal="${target}/.code-polishy-reports/behavior-review/intent-journal.json"
  assert_intent_capture_count "${target}" 2
  grep -Eq '"candidate_state_sha256": "[0-9a-f]{64}"' "${journal}" ||
    fail "${scenario}: dirty correction capture lacks its candidate-state digest"
  "${git_executable}" -C "${target}" commit --quiet -m "retain rejected correction residue"

  fixture_pass "${target}" "${scenario}" prepare-residue 1 behavior-review prepare --base "${base}"
  assert_packet_v4 "${target}" true
  write_final_state_finding_result "${target}" "internal/greeting/greeting.go" \
    correction-residue "The rejected broccoli path remains as a feature switch."
  fixture_expect_rejection "${target}" "${scenario}" finalize-residue 1 2 behavior-review finalize --base "${base}"
  grep -Fq 'final-state finding' "${output}" ||
    fail "${scenario}: finalization did not explain the final-state block: $(excerpt)"
  fixture_status "${target}" "${scenario}" "${base}" "FAILED (all changes)"
  grep -Fq 'internal/greeting/greeting.go:' "${output}" ||
    fail "${scenario}: failed status omitted the actionable final-state location"

  commit_requested_greeting "${target}" "${git_executable}" "remove rejected correction residue"
  record_requested_behavior_review "${target}" "${scenario}" "${base}" final-state-proof greeting-contract '[]' true
  fixture_gate_pass "${target}" "${scenario}" checkpoint "${base}" "PASSED (all changes)" 2 checkpoint-gate
  fixture_gate_pass "${target}" "${scenario}" merge "${base}" "PASSED (all changes)" 2 merge-gate
  fixture_summary "${target}" "${scenario}" 2
}

exercise_opt_in_behavior_review_fixtures() {
  local scratch_root="$1" git_executable="$2"
  exercise_no_config_behavior_review "${scratch_root}" "${git_executable}"
  exercise_capture_time_features_and_replay "${scratch_root}" "${git_executable}"
  exercise_later_additive_require "${scratch_root}" "${git_executable}"
  exercise_merge_required_feature "${scratch_root}" "${git_executable}"
  exercise_checkpoint_required_feature "${scratch_root}" "${git_executable}"
  exercise_strict_full_candidate "${scratch_root}" "${git_executable}"
  exercise_stale_fix_and_rereview "${scratch_root}" "${git_executable}"
  exercise_multi_task_union "${scratch_root}" "${git_executable}"
  exercise_dirty_correction_and_final_state "${scratch_root}" "${git_executable}"
}
