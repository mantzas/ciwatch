## Context

`ciwatch` is a Bubble Tea terminal application that renders GitHub Actions runs through a Bubbles table styled with Lip Gloss. The current UI shows repository, context, workflow, status, ref, event, age, duration, and title/SHA rows, plus a simple header and recent events. The data model already distinguishes broken, running, ok, neutral/no-run, and error states, so this change can improve usefulness by changing presentation rather than fetch, cache, notification, or sorting behavior.

The main constraint is terminal readability. The UI should be vibrant and colorful, but color must reinforce meaning and must not become the only way to understand status.

## Goals / Non-Goals

**Goals:**

- Make current CI health visible before the user reads individual rows.
- Use a consistent, vivid color language for status, focus, rate risk, refresh state, and event severity.
- Preserve the compact, keyboard-driven terminal workflow.
- Keep the implementation inside the existing Bubble Tea, Bubbles table, and Lip Gloss stack.

**Non-Goals:**

- No web UI, daemon, new persistence, or new GitHub API behavior.
- No changes to configuration, notification dedupe, cache format, or `--once` output.
- No new styling dependency.
- No rework of row ordering, run compaction, or keyboard bindings beyond clearer presentation of existing actions.

## Decisions

1. **Add a status summary strip above the table.**

   Rationale: A count strip gives the user the answer to "what is on fire?" before table scanning. It should derive from the currently rendered rows and include broken, running, ok, quiet/no-run, and error counts.

   Alternative considered: Only improve row colors. That makes rows prettier but still requires scanning the whole table to understand global health.

2. **Keep status text explicit and pair it with color.**

   Rationale: Labels such as `BROKEN`, `RUNNING`, `OK`, `ERROR`, and `QUIET` remain readable in low-color terminals, screenshots, and copied output. Color becomes a fast visual cue, not the only signal.

   Alternative considered: Icon-only badges. That would save width but weaken accessibility and make terminal output less self-explanatory.

3. **Use vivid semantic colors with restrained surfaces.**

   Rationale: Broken/error rows need hot red or coral, running needs amber/yellow, ok needs green, quiet needs muted gray-blue, and selected focus needs cyan or violet. The rest of the layout should stay neutral enough that the semantic colors carry signal.

   Alternative considered: A heavily themed full-screen palette. That risks reducing scanability and making long-running terminal use tiring.

4. **Style the existing table rather than replacing the layout.**

   Rationale: The table already encodes useful columns and row grouping. Improving header, selected row, cell styles, and status/event renderers is smaller and lower risk than introducing a new layout component.

   Alternative considered: Replace the table with custom row rendering. That may offer more control, but it expands the change surface and risks regressing navigation, selection, and resize behavior.

5. **Keep neutral internal status semantics, but improve user-facing wording.**

   Rationale: Internal `StatusNeutral` can continue to represent cancelled, skipped, no-run, or non-actionable states. The UI can present quiet/no-run states with clearer language without changing sorting or classification.

   Alternative considered: Split `StatusNeutral` into multiple model statuses. That is unnecessary for this visual improvement unless future behavior requires distinct actions.

## Risks / Trade-offs

- [Risk] ANSI styling can interfere with width calculations in table cells. -> Mitigation: keep styled labels within fixed-width status cells and add tests around rendered text without depending on exact escape sequences where possible.
- [Risk] A vibrant palette can reduce readability on some terminal themes. -> Mitigation: keep labels explicit, use standard terminal color indices or conservative adaptive colors, and avoid relying on background colors for critical meaning.
- [Risk] Summary counts can drift from table semantics if computed separately. -> Mitigation: derive counts from the same `m.rows` data used by `applyRows`.
- [Risk] Tests become brittle if they assert full rendered output. -> Mitigation: test focused strings and behavior, and isolate helpers for status labels and summary text.

## Migration Plan

Implement as a normal TUI rendering change. No data migration, config migration, or cache migration is required. Rollback is a code revert because no persisted state changes are introduced.

## Open Questions

- Should the neutral user-facing label be `QUIET`, `NO RUNS`, or continue to vary by row kind?
- Should row-level API errors be counted separately from broken workflow runs in the summary strip, or should they both contribute to an urgent count while still showing separate labels?
