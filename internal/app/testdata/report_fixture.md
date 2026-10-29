<!-- report 6efb743a -->
## Result: sort bags (#640)

Sorted the bags by weight. Ties keep their input order.

- Added `sort_bags` in `/example/repo/src/bags.rs`.
  - The sort is stable.
  - Old callers need no change.
- Tests: 12 passed, 0 failed.

DECISION: Keep ties in input order? | yes (recommended) | no

```rust
fn sort_bags(bags: &mut [Bag]) {
    bags.sort_by_key(|bag| bag.weight);
}
```

| Bag | Weight |
|-----|--------|
| A   | 3      |
| B   | 1      |

1. Read the spec.
2) Wrote the code.

> Note: the table is for the owner.

---
### Next
- Open the PR.
