# ng-web -- Testing

## Testing

Test philosophy: `.claude/references/test-rules.md`.

Baseline capture (Healthy Baseline Rule, root `AGENTS.md`): when a test fails after your change, run `npm test -- --watch=false` on a `git stash -u` baseline and on your change, and diff both outputs under `.me/<slug>/logs/` (recipe: `.claude/references/test-rules.md` rule 7). If proven pre-existing, STOP and ASK.

### Test fixtures

Shared fixtures go in src/testing/fixtures (create it with the first shared fixture). Do NOT pollute examples or production directories with test data.
