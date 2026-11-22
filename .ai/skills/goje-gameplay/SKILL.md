---
name: goje-gameplay
description: Design or change gameplay, input, controls, cameras, movement, combat, AI, or RTS unit commands in Goje games and playground scenarios.
---

# Gameplay development

1. Describe the visible behavior and how to demonstrate it in a small playable scenario. Test the rules independently from rendering and input devices.
2. Translate device events into named actions or tick-stamped commands at the input boundary. Specify press, release, hold, and ordering semantics. Do not apply a one-shot input once per catch-up tick.
3. If the feature requires reproducibility, record the initial state, simulation seed, and commands; replay them at fixed ticks. Use stable tie-breaks for simultaneous actions and path or target choices. Do not assume seeded randomness, floating-point math, or container iteration produces cross-platform replay by itself.
4. For 2.5D mechanics, identify ground position, elevation, and screen position separately. Test overlapping sprites, occlusion, and picking with elevated objects. Keep gameplay collision rules independent of draw order.
5. For RTS behavior, exercise one unit, many units, and congested or unreachable goals. Define cancellation, replacement, and completion of commands. Check that idle or off-screen units do not change rules just because presentation changes.
6. Verify the playable scenario on affected targets, including focus loss and variable frame rate when relevant. Record the expected outcome, not only that the app launches.
