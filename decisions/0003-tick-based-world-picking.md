# ADR 0003: Pick against the consuming simulation tick

**Status:** Accepted for M2. See [RFC 0003](../rfcs/0003-m2-camera-depth-and-picking.md) for scope and [the world-view specification](../specs/world-view-m2.md) for behavior and acceptance checks.

## Context

Goje's hosts can perform zero to four fixed updates between presentations. A primary mouse press may arrive when the displayed frame describes a different world state from the one the next update will see. Both approaches can be internally consistent, but they give different answers for moving units or a changing camera.

## Decision

Resolve the buffered press at the start of the tick that consumes it. Use that tick's starting camera and world order, before movement or camera updates. Hosts normalize coordinates and buffer the press; they do not perform hit tests. The engine supplies the ground-anchor order and reverse hit query; the scene owns selection and decides what a hit means for gameplay.

Do not retain a picking snapshot of the last submitted frame. This keeps selection testable in headless runs using the same scene and input snapshot and avoids introducing a second ownership path across frame presentation, scene restart, and resize.

## Consequences

A click during catch-up might not select the object visible at that point in the previous displayed frame. Draw and pick agree on the ordering of the same scene state, not on which state was visible when the mouse event arrived. If playtests show that mismatch matters, revisit frame-based picking with explicit snapshot ownership and a rule for when a snapshot expires.
