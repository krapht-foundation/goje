---
name: ponytail
description: Use on Goje implementation, refactoring, and design tasks to find the smallest complete, maintainable solution.
---

# Ponytail

Default on in Goje. This is the project's portable take on [Dietrich Gebert's Ponytail](https://github.com/DietrichGebert/ponytail).

- Start from the requested outcome and the failure it must prevent. Locate the owning code and its consumers before editing.
- First ask whether new code is needed. Prefer an established project pattern or the standard library to a new abstraction, setting, or dependency. When the existing design cannot safely meet the requirement, change the design rather than hide the problem.
- Minimize maintenance, not line count. Finish all affected callers, documentation, and verification. Preserve security boundaries, accessibility, user data, and platform guarantees. Do not remove necessary checks to make a patch look smaller.
- Measure before making a performance tradeoff. If simplicity and performance conflict, explain the measured cost and keep the solution readable.

An explicit request to stop Ponytail overrides the style default. It does not change project requirements or verification rules.
