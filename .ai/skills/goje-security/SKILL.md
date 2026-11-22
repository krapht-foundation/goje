---
name: goje-security
description: Defensively design or review Goje trust boundaries, asset decoders, project files, desktop and browser capabilities, and dependency or release security.
---

# Defensive security review

1. Identify inputs, privileges, and failure effects. Treat downloaded projects, assets, saves, URLs, and any future multiplayer data as untrusted. Keep secrets out of web builds, logs, asset bundles, test fixtures, and crash reports.
2. Bound input bytes, record counts, nesting, decoded size, decompression, and concurrent work as applicable. Validate before allocation or side effects; perform checked size arithmetic and reject unsupported versions clearly. Test accepted, invalid, and over-budget inputs with synthetic fixtures; fuzz pure parsers with bounded seed cases and resource use.
3. Confine imported files to an explicitly chosen root. Use `os.Root` for applicable Go filesystem operations and define symlink, overwrite, and archive-entry policies. Prefer staged imports and atomic replacement where supported so failed operations do not corrupt user projects. Do not execute asset contents or install executable extensions implicitly.
4. Give each platform only the capabilities it needs. Browser sandboxing is not authorization; avoid unnecessary JS bridges or untrusted HTML. If networking exists, use standard protocol and cryptographic libraries; authorize on the trusted side and do not trust a game client's privilege or state claims.
5. Use a supported Go patch release and run `make vuln` to scan the current target with `govulncheck`. Review build-time tools and dependencies as well as runtime code. Keep sensitive scanner evidence and private reports in restricted channels. Follow `SECURITY.md` for disclosure.
