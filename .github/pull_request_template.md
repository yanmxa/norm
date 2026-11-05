## What changed, and why it needed to

<!-- If a measurement drove this, put the number here. It is the part that will
     still be useful in a year. -->

## Checklist

- [ ] `make check` passes (gofmt, vet, race tests, and norm on itself)
- [ ] New behaviour has a test whose name states the invariant
- [ ] If this fixes something that went wrong once, a comment says so
- [ ] `norm.json` thresholds are unchanged, or lowered — never raised
