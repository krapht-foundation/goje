---
name: goje-knowledge-sync
description: Reconcile goje project documentation and agent context after changing behavior, an architecture decision, or the development workflow.
---

# Reconcile project knowledge

1. Read the change and any linked issue or accepted decision. Identify changes a game developer, tool user, or contributor would observe. Include desktop and web where applicable.
2. Use `.ai/README.md` to find the owning source for each change. Update the contract, decision record, or procedure there. If implementation conflicts with an accepted decision, report the conflict; do not rewrite the decision without approval.
3. Update or remove stale agent references. Link to the source instead of copying its requirements. Resolve working handoffs for completed work; preserve open questions in the issue or proposal that owns them.
4. Verify changed references and run the documented check that exercises the affected behavior, if one exists. Report unverified targets or unresolved discrepancies.
