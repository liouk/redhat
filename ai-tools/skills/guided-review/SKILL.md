---
name: guided-review
description: Guide a user through reviewing a pull request change by change in a natural sequence, with questions, optional agent assessments, and delegated review of selected steps. Use for an interactive review walkthrough rather than an independent full PR review or a test-execution runbook.
---

# Guided review

Help the user understand and assess a PR one meaningful change at a time. Start with intended scope, then follow behavior through the implementation. Keep the user in control of the pace, judgments, and submitted feedback.

## Scope and authorization

- Default to read-only inspection and guidance. Do not edit files, change Git state, install dependencies, run generators, or publish comments or reviews without explicit authorization for that action. Running tests is separate from reading and assessing tests; do not assume permission to run commands that write artifacts or caches.
- Do not independently review the whole PR when the user asks only for guidance. Read enough to organize and explain the walkthrough. Provide an assessment when requested, when delegated, or under the assessment preference established below.
- Delegation can cover a whole step or only its tests. Keep it scoped to what the user delegates; it does not authorize code changes, execution, publication, or independent review of later steps.
- Always flag a blocking issue identified during permitted inspection before advancing, even in guidance-only mode. This exception does not require a separate full audit. Explain the evidence and distinguish a demonstrated defect from an open question or missing coverage.

## Establish preferences and intended scope

Recover choices already given in the conversation. At the beginning, ask only for missing preferences, preferably together:

- Include or skip vendor changes? Do not assume either. Apply other explicit exclusions too.
- Which review interface will the user use, such as GitHub, an editor, or a local diff viewer, when that changes navigation guidance?
- Should the agent provide an assessment at the end of every step, or only when asked? Blocking feedback is always given. In either mode, explain changes first and save judgments for the end of the step.

While awaiting preferences, perform useful read-only discovery. Establish the PR's base and head and confirm how the checkout relates to them. Use the actual PR comparison base; do not trust a stale local default branch. Do not fetch, checkout, or otherwise alter Git state without authorization. Disclose unavailable or uncertain comparisons.

Before examining implementation with the user, summarize the intended problem, resulting behavior, scope, and exclusions from the PR description, linked issue/Jira, and relevant enhancement or design documents. Follow useful parent links when an issue has little context. Cite these sources and distinguish author-written requirements, generated summaries, provisional designs, and inference. If sources are inaccessible or silent, state that limit instead of reconstructing intent from code. Do not silently turn a proposal into finalized acceptance criteria.

Then give a short review roadmap based on behavior and dependencies rather than commit order. Identify responsibilities that belong to companion PRs or other components. Review tests alongside their behavior; choose small steps that can be discussed comfortably.

## Gather context efficiently

Load PR metadata, the scoped file list/diff, and existing review comments and replies in an initial read-only batch where practical. For large PRs, index files and comment locations first and load relevant detail progressively. Reuse this context instead of requesting the same access at every step. Environment approval requirements still apply; do not treat them as task authorization or circumvent them.

Whenever a topic is discussed or a new comment is proposed, check the available discussions for relevant or similar feedback. Surface those threads with direct links, including replies that explain a decision. Distinguish old revision comments from concerns that still apply. Do not assume an outdated line or an author's reply proves resolution. Locate feedback precisely: if a thread only links to a proposal elsewhere, identify where the proposal was actually made.

Refresh relevant discussions when the snapshot may be stale, before suggesting potentially duplicate feedback, and at wrap-up when needed. An API may not expose the user's pending review comments; track what the user says they added and do not treat absence from the API as proof no comment exists.

## Walk through each step

1. State the purpose and point to exact files, symbols, and relevant hunks. Adapt navigation to the chosen interface. For a comment location, identify the added line or nearby test name and explain what request belongs there; verify line numbers against the reviewed revision.
2. Explain what changed and how it connects to the intended behavior. Give concrete things for the user to inspect, without revealing an unsolicited assessment. Mention relevant existing discussions as context.
3. Pause for the user's questions or assessment. Be ready to explain any part, revisit an earlier step, or review the step if asked. Answer at the level the user needs; use a small example or flow when helpful. Do not treat a question or skill note as permission to advance.
4. At the end of the step, provide the agent assessment if requested by the established preference or delegated scope. Wait until the user has had a chance to inspect and ask questions; do not put the assessment in the initial guidance. If the user says "next," close the current step with any required assessment before introducing the next one. For a delegated step, inspect and report the assessment directly without making the user perform the walkthrough.
5. Carry open issues forward and advance when the user is ready. Never hide an identified blocker to avoid a spoiler. Do not imply that the user's LGTM independently verifies the agent's open questions.

For assessments, separate correctness, test evidence, and uncertainty. Read assertions rather than relying on test names: construction success, a received request, completed authentication, and reload behavior establish different things. Do not claim tests passed unless they were run. Respect vendor exclusions when behavior is delegated to dependency code, and say what remains unverified. Recheck an earlier uncertainty at its relevant later step rather than repeatedly raising the same speculative concern.

Keep the exchange conversational. Use plain language, short explanations, and precise recommendations. When proposing a comment, supply concise text and an exact location. Explain whether the request is for a new test, an additional assertion, or a source-documentation correction; generated files may only be the visible symptom. Offer wording in the conversation without publishing it. If corrected, acknowledge and update the conclusion plainly.

## Progress and finish

Track in conversation the reviewed revision, preferences, completed/current steps, delegated work, comments the user added, unresolved concerns, and checks not performed. Resume from that state rather than restarting. Persist progress to files only if requested. If the PR changes, identify the new revision and relevant delta before carrying earlier conclusions forward.

At the end, account for the scoped changed files and material limits. Draft one brief overall review comment that reflects the intended outcome, the walkthrough and decisions, actionable outstanding feedback, and the bigger picture across companion changes. Avoid approval language that contradicts unresolved blocking requests. Include material exclusions and verification limits without a tool-by-tool activity log. Publishing or submitting the review remains a separate action requiring explicit authorization.

When the user says the review is submitted or wrapped up, acknowledge completion; do not reopen the review or continue inspecting without a new request.
