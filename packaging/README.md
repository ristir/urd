# Cutting a release

Notes to future me. Not user-facing documentation — that's `README.md` in the root.

## The short version

```
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

`.github/workflows/release.yml` does the rest: runs the suite, builds the four
archives with `make dist`, checks that the binaries really report `vX.Y.Z`, renders
the Homebrew formula and publishes the GitHub Release with everything attached.

The tag must be on a clean tree. `make dist` takes the version from
`git describe --tags --always --dirty`, so a dirty tree gives archives named after a
`-dirty` version — the workflow's version check fails on that rather than shipping it
under the tag's name.

## What lands on the release

- `urd_vX.Y.Z_{darwin,linux}_{arm64,amd64}.tar.gz` — each with `urd`, `README.md`,
  `uninstall.sh` and `LICENSE`
- `checksums.txt`
- `urd.rb` — the rendered formula, so `brew install --formula <that URL>` works even
  with no tap

## Doing it by hand

```
make dist
sh packaging/formula.sh vX.Y.Z dist > dist/urd.rb
```

`formula.sh` fills `packaging/urd.rb`, which is a template, from `dist/checksums.txt`,
and refuses to write a formula with a placeholder or an empty sha256 left in it.
`GITHUB_REPOSITORY` overrides the repo in the URLs; it defaults to `ristir/urd`.

Testing the formula before it goes anywhere:

```
brew install --build-from-source ./dist/urd.rb
brew test urd
brew uninstall urd
```

To test without a published release, point a `url` at
`file:///absolute/path/to/dist/urd_....tar.gz` first.

## The Homebrew tap

The release workflow pushes `Formula/urd.rb` to a tap, but only when both of these
exist — until then that step is skipped and the release still happens:

- repository **variable** `HOMEBREW_TAP` = `ristir/homebrew-urd` (the owner/repo of the
  tap; Homebrew maps `brew tap ristir/urd` onto exactly that name)
- repository **secret** `HOMEBREW_TAP_TOKEN` = a PAT with `contents: write` on the tap
  repo. `GITHUB_TOKEN` cannot reach another repository.

Once the tap exists, the root `README.md` needs its `brew tap ristir/urd && brew install
urd` line — the Install section is the only place that mentions installing.
