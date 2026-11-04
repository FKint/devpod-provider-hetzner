# Instructions for Claude

## Go file license headers

This repo is a fork. Every `.go` file carries an Apache-2.0 header, enforced by
the `license-eye` pre-commit hook (config in `.licenserc.yaml`).

- Never remove or replace the existing upstream line
  `Copyright 2023 Simon Emms <simon@simonemms.com>`.
- When you modify an existing `.go` file that only has the upstream line, add a
  second line directly below it:

  ```go
  /*
   * Copyright 2023 Simon Emms <simon@simonemms.com>
   * Copyright 2026 github.com/FKint/devpod-provider-hetzner Authors
   *
   * Licensed under the Apache License, Version 2.0 (the "License");
  ```

  The year is the year of the first change in this fork (2026), not the current
  year. Files that already have the second line need no change.
- New `.go` files get the fork-only header (`Copyright 2026
  github.com/FKint/devpod-provider-hetzner Authors`). `license-eye header fix`
  generates it.
- `license-eye` cannot tell that a modified file is missing the second line, so
  check this by hand before committing.
