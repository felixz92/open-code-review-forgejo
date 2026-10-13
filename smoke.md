# 0. Build the binary
go build -o ocr-forgejo ./cmd/ocr-forgejo

# 1. Create a small result fixture (a real `.ocr/ocr-result.json` works too)
cat > /tmp/ocr-fixture.json <<'EOF'
{"comments":[
{"path":"some/file.go","start_line":10,"end_line":10,"content":"inline finding","category":"bug","severity":"high"},
{"path":"some/file.go","start_line":1,"end_line":99,"content":"out of diff finding","category":"bug","severity":"low"},
{"path":"","content":"no line info finding","category":"style","severity":"low"}
],"message":"done","manifest":{"input":{"resolved_head":"fd68020e06513e4ed50a5169f417914eba9fcc18"}}}
EOF

Use the PR's current head SHA for resolved_head — that's what commit binding uses.
# 2. First run (expect: 1 inline comment, out-of-diff + no-line in summary)
GIT_TOKEN="$(k get secret forgejo-review-bot -o jsonpath='{.data.password}' | base64 -d)" ./bin/ocr-forgejo \
--url https://forgejo.zippelf.de/ --repo felix/scripts --pr 1 \
/tmp/ocr-fixture.json --stats-file /tmp/ocr-stats.env

# 3. Second run unchanged (expect: same summary comment updated in place,
#    no second review; with --incremental the inline comment is skipped instead)
GIT_TOKEN="$(k get secret forgejo-review-bot -o jsonpath='{.data.password}' | base64 -d)" ./bin/ocr-forgejo \
--url https://forgejo.zippelf.de/ --repo felix/scripts --pr 1 \
--incremental /tmp/ocr-fixture.json --stats-file /tmp/ocr-stats.env

# 4. Check the stats file
cat /tmp/ocr-stats.env
What to look for:
- Run 2 normal: exactly one review per run, the summary comment edited (not duplicated)
- Run 2 with --incremental: OCR_COMMENTS_SKIPPED=1, OCR_COMMENTS_INLINE=0, summary notes the skip
- The out-of-diff comment shows as "out of diff (line could not be resolved)" in the summary on both runs
## Observed results (2026-10-13, forgejo.zippelf.de, felix/scripts PR 1)

All runs against the fixture above; two issues found and fixed during smoke:

1. Forgejo rejects PENDING review creation with an empty body -> fixed: creation now sends a one-line body.
2. The PR's diff contains only an empty new file (hunkless) -> parser rejected it; fixed: hunkless diffs are accepted as an incomplete inventory.

- Run without `--incremental`: clean pre-check, `2/3 inline` posted, no-line comment in summary, `0 failed`, no duplicate summary.
- Run with `--incremental`: `0/3 inline`, `2 skipped` (both previously posted comments overlap), summary notes the skip.
- Sticky summary confirmed updated in place across runs (no second summary comment, no duplicate reviews).
- Dry-run and severity gating verified separately (exit 3 on `--fail-on-severity low`).
