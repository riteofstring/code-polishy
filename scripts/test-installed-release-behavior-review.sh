#!/usr/bin/env bash

# Sourced by test-installed-release.sh after its fixture helpers are defined.

write_single_review_result() {
  local target="$1" classification="$2" proof_ids="$3" before="$4" after="$5"
  local packet="${target}/.code-polishy-reports/behavior-review/packet.json"
  local result="${target}/.code-polishy-reports/behavior-review/result.json"
  local review_id base candidate intent_sha
  review_id="$(lock_field "${packet}" review_id)"
  base="$(lock_field "${packet}" base)"
  candidate="$(lock_field "${packet}" candidate)"
  intent_sha="$(lock_field "${packet}" intent_sha256)"
  if [[ ! "${review_id}" =~ ^review-[0-9a-f]{32}$ ||
    ! "${base}" =~ ^[0-9a-f]{40,64}$ ||
    ! "${candidate}" =~ ^[0-9a-f]{40,64}$ ||
    ! "${intent_sha}" =~ ^[0-9a-f]{64}$ ]]; then
    fail "installed behavior review packet has invalid identifiers"
  fi
  write_file "${result}" <<EOF
{
  "version": 2,
  "review_id": "${review_id}",
  "base": "${base}",
  "candidate": "${candidate}",
  "intent_sha256": "${intent_sha}",
  "behaviors": [
    {
      "before": "${before}",
      "after": "${after}",
      "classification": "${classification}",
      "proof_ids": ${proof_ids}
    }
  ],
  "findings": []
}
EOF
}

write_requested_review_result() {
  write_single_review_result "$1" requested "[\"$2\"]" \
    "Render returns the old greeting." \
    "Render returns the requested new greeting."
}

write_multi_task_review_result() {
  local target="$1" proof_id="$2"
  local packet="${target}/.code-polishy-reports/behavior-review/packet.json"
  local result="${target}/.code-polishy-reports/behavior-review/result.json"
  local review_id base candidate intent_sha
  review_id="$(lock_field "${packet}" review_id)"
  base="$(lock_field "${packet}" base)"
  candidate="$(lock_field "${packet}" candidate)"
  intent_sha="$(lock_field "${packet}" intent_sha256)"
  write_file "${result}" <<EOF
{
  "version": 2,
  "review_id": "${review_id}",
  "base": "${base}",
  "candidate": "${candidate}",
  "intent_sha256": "${intent_sha}",
  "behaviors": [
    {
      "before": "Render returns the old greeting.",
      "after": "Render returns the requested new greeting.",
      "classification": "requested",
      "proof_ids": ["${proof_id}"]
    },
    {
      "before": "Render builds the greeting directly.",
      "after": "Render uses a named prefix with the same output.",
      "classification": "preserved",
      "proof_ids": []
    }
  ],
  "findings": []
}
EOF
}

dogfood_pass() {
  local target="$1" description="$2" phase="$3"
  shift 3
  local started=${SECONDS}
  expect_pass "${target}" "${description} ${phase}" "$@"
  printf 'DOGFOOD PASS repo=%s phase=%s duration_seconds=%d\n' \
    "${description}" "${phase}" "$((SECONDS - started))"
}

record_requested_behavior_review() {
  local target="$1" description="$2" base="$3" proof_id="$4"
  dogfood_pass "${target}" "${description}" prepare \
    behavior-review prepare --base "${base}"
  dogfood_pass "${target}" "${description}" regression-proof \
    regression-proof --base "${base}" --suite greeting-unit \
    --evidence internal/greeting/greeting_test.go --id "${proof_id}"
  write_requested_review_result "${target}" "${proof_id}"
  dogfood_pass "${target}" "${description}" finalize \
    behavior-review finalize --base "${base}"
}

exercise_behavior_review_lane() {
  local target="$1" description="$2" git_executable="$3" scratch_root="$4" output_path="$5"
  local base intent preserved_base preserved_intent unintended_base unintended_intent
  base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  intent="${scratch_root}/${description}-intent.txt"
  preserved_intent="${scratch_root}/${description}-preserved-intent.txt"
  unintended_intent="${scratch_root}/${description}-unintended-intent.txt"
  printf '%s\n' 'Change Render to return "hi" and keep that behavior covered.' >"${intent}"
  dogfood_pass "${target}" "${description}" capture-intent \
    behavior-review capture-intent --intent-file "${intent}"

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
  "${git_executable}" -C "${target}" commit --quiet -m "requested greeting behavior"

  record_requested_behavior_review "${target}" "${description}" "${base}" greeting-change
  dogfood_pass "${target}" "${description}" checkpoint \
    checkpoint-gate --base "${base}"

  record_requested_behavior_review "${target}" "${description}" "${base}" greeting-change
  dogfood_pass "${target}" "${description}" merge \
    merge-gate --base "${base}"

  preserved_base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  printf '%s\n' 'Refactor Render without changing its output.' >"${preserved_intent}"
  dogfood_pass "${target}" "${description}" capture-preserved-intent \
    behavior-review capture-intent --intent-file "${preserved_intent}"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

const greetingPrefix = "hi, "

func Render(name string) string {
	return greetingPrefix + name
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go
  "${git_executable}" -C "${target}" commit --quiet -m "preserve greeting behavior"

  expect_findings "${target}" "${description} stale candidate" merge-gate --base "${base}"
  expect_finding "${description} stale candidate" "policy.behaviorReview" \
    ".code-polishy-reports/behavior-review/receipt.json" "gate-receipt"
  printf 'DOGFOOD PASS repo=%s phase=stale-candidate-rejected duration_seconds=0\n' "${description}"

  dogfood_pass "${target}" "${description}" prepare-preserved \
    behavior-review prepare --base "${preserved_base}"
  write_single_review_result "${target}" preserved '[]' \
    "Render builds the greeting directly." \
    "Render uses a named prefix with the same output."
  dogfood_pass "${target}" "${description}" finalize-preserved \
    behavior-review finalize --base "${preserved_base}"
  dogfood_pass "${target}" "${description}" checkpoint-preserved \
    checkpoint-gate --base "${preserved_base}"

  dogfood_pass "${target}" "${description}" prepare-multi-task \
    behavior-review prepare --base "${base}"
  dogfood_pass "${target}" "${description}" prove-multi-task \
    regression-proof --base "${base}" --suite greeting-unit \
    --evidence internal/greeting/greeting_test.go --id greeting-change
  write_multi_task_review_result "${target}" greeting-change
  dogfood_pass "${target}" "${description}" finalize-multi-task \
    behavior-review finalize --base "${base}"
  dogfood_pass "${target}" "${description}" merge-multi-task \
    merge-gate --base "${base}"

  unintended_base="$("${git_executable}" -C "${target}" rev-parse HEAD)"
  printf '%s\n' 'Rename the internal prefix without changing behavior.' >"${unintended_intent}"
  dogfood_pass "${target}" "${description}" capture-unintended-intent \
    behavior-review capture-intent --intent-file "${unintended_intent}"
  write_file "${target}/internal/greeting/greeting.go" <<'EOF'
package greeting

const greetingPrefix = "broken, "

func Render(name string) string {
	return greetingPrefix + name
}
EOF
  "${git_executable}" -C "${target}" add internal/greeting/greeting.go
  "${git_executable}" -C "${target}" commit --quiet -m "unintended greeting behavior"
  dogfood_pass "${target}" "${description}" prepare-unintended \
    behavior-review prepare --base "${unintended_base}"
  write_single_review_result "${target}" unintended '[]' \
    "Render returns the requested greeting." \
    "Render returns an unrequested broken greeting."
  if run_policy "${target}" behavior-review finalize --base "${unintended_base}"; then
    fail "${description}: unintended behavior review finalized successfully"
  fi
  grep -q 'reviewer classified a behavior as unintended' "${output_path}" ||
    fail "${description}: unintended review did not explain the block: $(excerpt)"
  printf 'DOGFOOD PASS repo=%s phase=unintended-change-blocked duration_seconds=0\n' "${description}"
}
