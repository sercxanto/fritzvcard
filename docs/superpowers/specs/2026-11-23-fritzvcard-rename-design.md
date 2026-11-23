# Rename fritzbook to fritzvcard

## Intent

Use `fritzvcard` as both the project name and the primary CLI executable name. The tool remains a local vCard-to-FRITZ!Box converter/uploader; no network behavior changes.

## Constraints

- Rename active CLI, Go module/imports, build outputs, release assets, runtime-generated names, current documentation and tests.
- Keep `fritzprobe` as the separately named probe. Never contact a device.
- Keep historical specs/plans as records of their original implementation; do not rewrite their filenames or descriptions retroactively.
- Preserve existing backup files and git-ignore protection for the former prefix.
- Keep existing DevContainer volume mounts to avoid hiding persisted user state; change only its visible project name.
- Repository rename on GitHub is a separate owner action.

## Approaches

- Rename only the binary: small change but contradicts the request that project and CLI names match.
- Rename active project and CLI surfaces (chosen): coherent visible name and module path while preserving historical records and existing state.
- Replace every occurrence, including probe and old plans: risks breaking historical references and persisted volume access for no benefit.

## Verification

Exercise CLI usage/version/help, backup names, export/import artifacts, all Go tests and lint, cross-build outputs, and release asset paths. Do not use a real box.
