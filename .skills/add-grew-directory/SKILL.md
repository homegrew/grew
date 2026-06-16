---
name: add-grew-directory
description: Adds a new system directory to the grew configuration so it is created during grew setup.
---

# How to add a system directory to grew

When requested to add a new directory to the grew installation structure, follow these steps:

1. **Edit `pkg/config/paths.go`**:
   - Add the directory as a `string` field to the `Paths` struct. Document it with its location.
   - Initialize the directory in the `FromRoot` function. For example: `MyNewDir: filepath.Join(root, "my_new_dir")`.
   - Add the directory to the returned array in `InitDirs()`. For example: `{Name: "my_new_dir", Path: p.MyNewDir}`.

2. **Edit `pkg/config/paths_test.go`**:
   - Add the new `paths.MyNewDir` field to the slice of paths verified in `TestInit_CreatesDirectories()`.

3. **Document the structure**:
   - Whenever directories are directories or changed, ensure that `docs/tech.md` is updated to reflect the new directory structure.
