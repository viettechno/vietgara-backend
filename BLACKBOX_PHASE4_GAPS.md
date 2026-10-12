# Blackbox Test Gaps for Release Phase 4.1

*Generated: October 12, 2026*

## Summary

The traceability check (`python3 testing/blackbox/scripts/check_traceability.py`) reveals **29 missing scenarios** (13 P1, 10 P2, 6 P3) and **85 partial coverage** scenarios. API coverage is **83.9%** (passes the 80% gate), but 8 client methods are never called from tests.

---

## Missing P1 Scenarios (13) — Block Release 4.1 DoD

| ID | Title | Module | Notes |
|----|-------|--------|-------|
| **E2E-BIL-009** | Permissions: billing view and billing write | Settlement | Admin panel tests need separate context |
| **E2E-IAM-020** | Platform admin uses admin web; others denied | IAM | Requires seeded admin + backend restart |
| **E2E-INV-010** | Permissions: view-only, and full access | Inventory | |
| **E2E-LIC-005** | Platform admin creates plans; hidden and duplicate plans | License | Admin panel tests |
| **E2E-LIC-006** | Platform admin changes plan limits; owners see at once | License | Admin panel + owner web |
| **E2E-LIC-008** | Platform admin finds and extends subscription | License | Admin panel |
| **E2E-LIC-010** | Period ends: owner's garages become read-only | License | Needs SQL time helper H1 |
| **E2E-LIC-011** | Grace period ends: garages suspended; admin extends | License | Needs SQL time helper H2 |
| **E2E-QUO-012** | Daily transaction limit blocks quote creation | Quote | Needs faked clock + limit plan |
| **E2E-QUO-014** | Permissions: no access, and full access | Quote | |
| **E2E-REP-003** | Repair order from approved quote inherits data | Repair Order | |
| **E2E-REP-006** | Update line progress; order status derived | Repair Order | |
| **E2E-REP-012** | Permissions: no access, view-only, full access | Repair Order | |

**Infrastructure-dependent (need separate work):**
- E2E-LIC-010/011 — need SQL time helpers (H1/H2)
- E2E-QUO-012 — needs transaction limit plan + faked clock

---

## Missing P2 Scenarios (10)

| ID | Title | Module | Notes |
|----|-------|--------|-------|
| **E2E-BIL-010** | Daily transaction limit counts settlement creation | Settlement | |
| **E2E-IAM-005** | Wrong code, then too many wrong codes | IAM | |
| **E2E-IAM-010** | Session survives reload and expired access token | IAM | |
| **E2E-IAM-021** | Adding platform admin through configuration | IAM | Needs backend restart |
| **E2E-LIC-007** | Plan taken off sale cannot be chosen; subscribers keep it | License | |
| **E2E-LIC-009** | Expiry reminder e-mail | License | |
| **E2E-LIC-012** | Owner renews from read-only by choosing a plan | License | |
| **E2E-QUO-009** | Garage's default approval mode pre-fills new quote | Quote | |
| **E2E-REP-007** | Filter board by technician | Repair Order | |
| **E2E-TEN-018** | Role editor: unsaved changes, undo and reset | Tenant | |

---

## Partial Coverage (85 scenarios)

All 85 are marked "partial" — the test tag exists but the test may not fully cover all expected results. These need review to promote to "full".

Key modules with many partials:
- **Tenant**: 20 partials (TEN-001 through TEN-023)
- **Customer**: 12 partials (CUS-001 through CUS-012)
- **IAM**: 12 partials (IAM-001, 002, 003, 004, 006, 007, 008, 009, 011, 012, 014, 015, 018, 019)
- **Quote**: 10 partials
- **Inventory**: 9 partials
- **License**: 4 partials
- **Repair Order**: 6 partials
- **Billing**: 8 partials

---

## Client Methods Never Called from Tests (8)

These client methods exist but are never invoked from `scenarios/` or `fixture/` test code:

| Method | Likely Scenario |
|--------|-----------------|
| `CancelMyPendingPlanChange` | License P2 |
| `CreatePlan` | E2E-LIC-005 (P1) |
| `ExtendSubscription` | E2E-LIC-008 (P1) |
| `ListAllPlans` | E2E-LIC-005/006 (P1) |
| `ListSubscriptions` | E2E-LIC-008 (P1) |
| `LookupCustomers` | E2E-CUS-010/012 (P1) |
| `UpdatePlan` | E2E-LIC-006 (P1) |
| `UpdateSettlement` | E2E-BIL-005 (P1) |

---

## Uncalled API Endpoints (23)

```
DELETE /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles/{taxProfileId}
DELETE /api/v1/garages/{garageId}/files/{fileId}
DELETE /api/v1/me/subscription/plan-changes/pending
GET  /api/v1/admin/plans
GET  /api/v1/admin/subscriptions
GET  /api/v1/garages/{garageId}/customers/lookup
GET  /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles
GET  /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles/{taxProfileId}
GET  /api/v1/garages/{garageId}/files/{fileId}/url
GET  /api/v1/garages/{garageId}/staff-candidates
PATCH /api/v1/admin/plans/{planId}
PATCH /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles/{taxProfileId}
PATCH /api/v1/garages/{garageId}/settlements/{settlementId}
PATCH /api/v1/roles/{roleId}
POST /api/v1/admin/plans
POST /api/v1/admin/subscriptions/{subscriptionId}/extensions
POST /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles
POST /api/v1/garages/{garageId}/customers/{customerId}/tax-profiles/{taxProfileId}:activate
POST /api/v1/garages/{garageId}/customers/{customerId}/user-links/current:confirm
POST /api/v1/garages/{garageId}/files
```

Most relate to:
- **Admin plans/subscriptions** (E2E-LIC-005, 006, 008)
- **Customer tax profiles** (Release 5.0)
- **File uploads** (Release 5.0)
- **Role updates** (E2E-TEN-017, 018)

---

## Action Items for Release 4.1

### Must Do (P1 gaps)
1. Add blackbox tests for all 13 missing P1 scenarios
2. Promote partial → full for 85 scenarios
3. Call the 8 unused client methods from tests

### Infrastructure Needed
- [ ] SQL time helpers (H1, H2, H4) for LIC-010/011, TEN-012
- [ ] Transaction limit plan + faked clock for QUO-012, BIL-010
- [ ] Admin test context that works in CI (already fixed in vietgara-e2e)

### Documentation
- [ ] Update `documented-ids.txt` when new scenarios are added
- [ ] Regenerate traceability from vietgara-docs@latest

---

## Next Steps

1. **Create issues** for each missing P1 scenario
2. **Prioritize** tests that unblock client method coverage (CreatePlan, ExtendSubscription, ListAllPlans, ListSubscriptions, LookupCustomers, UpdatePlan, UpdateSettlement)
3. **Coordinate** with vietgara-e2e for admin-web tests (E2E-LIC-005, 006, 008, BIL-009, IAM-020, INV-010, REP-012)