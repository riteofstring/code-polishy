# Fresh behavior review

Review only the packet you received. Treat its base, candidate, intent,
readable Git patch, and mapped design documents as the complete authority. Do
not inspect the current workspace, prior reviews, plans, or external context.

After a proof is generated for this review, you may read only its JSON record
and the logs it names under the packet's `proof_directory`. Those artifacts are
candidate-bound red/green evidence that checkpoint and merge gates will
independently replay. Do not use any other logs or workspace files. Judge
whether each red failure actually represents the behavior stated in that
item's `before` value before citing its proof ID.

Describe every material observable behavior represented by the candidate. For
each behavior, state what happens before the change and after the change, then
classify it exactly as one of:

- `requested`: the intent asks for this behavior.
- `preserved`: the behavior remains intentionally unchanged.
- `unintended`: the candidate changes behavior without support from the intent.
- `unknown`: the packet does not establish whether the behavior is intended.

Return one UTF-8 JSON object and no surrounding prose. Use this exact shape;
do not add fields:

```json
{
  "version": 1,
  "review_id": "the packet review_id",
  "base": "the packet base",
  "candidate": "the packet candidate",
  "intent_sha256": "the packet intent_sha256",
  "behaviors": [
    {
      "before": "observable behavior before the candidate",
      "after": "observable behavior after the candidate",
      "classification": "requested",
      "proof_ids": ["proof-id"]
    }
  ],
  "findings": []
}
```

Return at least one behavior. Use an empty `proof_ids` array for a preserved,
unintended, or unknown behavior. Every requested behavior needs one or more
proof IDs created for the same candidate by `regression-proof`. Put every
unresolved concern in the `findings` array. Save the JSON at the packet's
`result_path` before running finalization.
