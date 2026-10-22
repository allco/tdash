# Checkout status · updated 16:11 CEST · merged: Parts 1–6, ADR-3 !812

## Ongoing work

Agents are asked for status every 5 minutes. All times are Europe/Stockholm.

| Who          | Doing                                                              | Last heard |
|--------------|--------------------------------------------------------------------|------------|
| Main session | P7 merged; P8 rebased + auto-merge; 9–11 restacked                 | 16:10      |
|--------------|--------------------------------------------------------------------|------------|
| Verify agent | P11: verify the receipt e-mail on the staging store                | 16:06      |
|              |                                                                    | (sent)     |
| Review agent | P10: second review pass after the split                            | 16:08      |

## Open stack

`main ← P8 !824 ← P9 !827 ← P10 !831 ← P11 !835`

| Part | MR   | Ap | CI | Threads | Local checks  | claude-review | /code-review | Status            |
|------|------|----|----|---------|---------------|---------------|--------------|-------------------|
| 8    | !824 | 🟢 | 🔄 | 💬2 ❗1 | ✅ full gates | ✅ clean      | ✅ clean     | 🚉 train after CI |
| 9    | !827 | 🟢 | ✅ | —       | ✅ full gates | ✅ clean      | ✅ gates     | ✅ ready to merge |
| 10   | !831 | —  | ✅ | 💬3     | ✅ full gates | 1 Minor →     | ✅ gates     | 👀 needs approval |
|      |      |    |    |         |               | fixed         |              |                   |
| 11   | !835 | —  | ❌ | 💬1     | ✅ full gates | 1 W/2 M       | —            | 📝 draft          |

## Plan

1. P8 on auto-merge; when it merges, restack 9–11 onto main.
2. Get a second approval on P10, then put it on the train.
3. Fix the failing receipt test in P11 and take it out of draft.

## Decisions in force

- `PaymentRepository` returns `PaymentOutcome` (`Success`, `Failure.Declined/Network/Malformed`);
  `RefundOutcome` lives next to it. `CancellationException` is rethrown.
- Repository methods are one-liners: settings URL → client factory, values → client,
  `.toPaymentOutcome()`. No URL guard in the repository.
- One HTTP client per request, built by a plain factory that parses the endpoint; single-method
  clients are `fun interface`s with lambda stand-ins. No singleton or cache.
- Logs and exception messages name only the outcome type, never the cause.
- Tests per call: every outcome, invalid and blank URL → `Malformed` with no request, and every
  rewritten caller branch.
- Part 10 splits `PaymentRepository` into `CardPayment`, `WalletPayment` and `Refunds`; one
  implementation and module each.
- Keep legacy behaviour where callers depend on it: a declined wallet payment still clears the
  basket.

## Open questions

- Part 11: send the receipt e-mail before or after the order is confirmed? Today it's after, which
  delays it by one round trip.

## Out of scope questions

- Move the flaky `CurrencyFormatterTest` to the nightly run in a separate MR?
- Replace the real-looking card numbers in the test fixtures with obviously fake ones?
