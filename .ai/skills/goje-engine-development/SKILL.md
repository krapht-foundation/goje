---
name: goje-engine-development
description: Design or change Goje engine subsystems, frame scheduling, 2.5D rendering, simulation, spatial queries, or desktop and browser integration.
---

# Engine development

- Define ownership and frame phases before changing them: input sampling, simulation, presentation, and resource submission. A UI frame is not a simulation tick. When using fixed ticks, bound catch-up and define behavior after pause, stalls, and browser tab suspension. Render interpolation must not mutate simulation state.
- Specify coordinates, units, and sort keys at boundaries. For 2.5D rendering, define the ground anchor and elevation contribution; use stable tie-breaks and test overlapping footprints. Test world/screen projection and picking on equivalent fixtures, including window resize and scaling.
- Keep simulation independently testable. Put platform differences at the platform boundary; do not expose native handles or browser APIs through shared game rules. Verify thread affinity and resource lifetime where a backend requires them. Cover initialization failure, disposal, and backend loss when the subsystem owns those transitions.
- For spatial queries, define update timing, boundary inclusion, and ordering guarantees. Compare a new index against a simple reference on small layouts and check dense layouts separately. Do not choose an ECS, renderer, or partitioning scheme without a demonstrated need.
- Verify headless invariants and run the actual desktop or browser path for timing, rendering, audio, or input changes. A cross-build cannot exercise runtime behavior. For hot-path changes, use the [performance workflow](../goje-performance/SKILL.md) from the canonical skill directory.
