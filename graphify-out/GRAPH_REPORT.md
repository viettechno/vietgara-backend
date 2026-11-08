# Graph Report - vietgara-backend  (2026-11-09)

## Corpus Check
- 1239 files · ~755,624 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 75 file(s) not represented in the graph (top: .tmpl 52, .mk 11, (none) 8)

## Summary
- 13605 nodes · 67256 edges · 293 communities (251 shown, 42 thin omitted)
- Extraction: 95% EXTRACTED · 5% INFERRED · 0% AMBIGUOUS · INFERRED: 3550 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `9bffc5f9`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- context.Context
- ToGRPCError
- go_pkg_context
- call
- Pool
- go_pkg_testing
- testing.T
- TimeToProto
- github.com/stretchr/testify/mock.Call
- Part
- NewVerifiedOwner
- New
- UserID
- github.com/stretchr/testify/mock.Mock
- Repo
- .Return
- QuerierFromContext
- server/main.go
- RequireAPIError
- go_pkg_time
- run
- Payable
- github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1.Session
- newUseCase
- toAppError
- run
- Plan
- newReportEnv
- RedisStore
- Receivable
- .Return
- apperror.go
- time.Time
- newEnv
- jobs_integration_test.go
- Scope
- Server
- Subscription
- Entitlements
- .Return
- UserLink
- .Return
- apiPolicies
- NewBusiness
- WithClaims
- Vehicle
- toAppError
- Quote
- preview.go
- Internal
- wiring.go
- .FindByID
- assertAppError
- Customer
- newUseCase
- StaffGroup
- TaxProfile
- github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1.RepairOrder
- .Return
- Role
- Entry
- .Return
- github.com/jackc/pgx/v5/pgconn.CommandTag
- preview_test.go
- UserSummary
- Garage
- newEnv
- newUseCase
- .Return
- newPool
- sync_test.go
- newPayableEnv
- Invitation
- Log
- DocumentType
- NewPool
- IsZero
- newEnv
- As
- Settlement
- SettlementView
- Message
- Party
- Settings
- PlanPaymentRequest
- setup
- quoteToProto
- Access
- ReceivableUseCase
- LocalStore
- .Return
- check_traceability.py
- net/http.ResponseWriter
- Store
- Request
- newEnv
- User
- .Return
- Validation
- newPublicLimit
- .Return
- newUseCase
- repairorder.go
- AdjustmentLine
- QuoteUseCase
- RepairOrder
- PartInfo
- PublicUseCase
- PortalUseCase
- rls_test.go
- .Return
- Garage
- InventoryUseCase
- PostgresUserRepository
- pdf.go
- Staff
- .Return
- .Return
- .Return
- NotificationUseCase
- portal_test.go
- quote/handler/mapping.go
- newGarageUseCase
- StockTransaction
- toAppError
- repairOrderToProto
- Supplier
- StockCount
- UseCase
- dataimport/usecase/ports.go
- Event
- newEnv
- .Return
- .Return
- flow.js
- Type
- Period
- GarageHeader
- CustomerInfo
- UserInfo
- Issuer
- newUseCase
- Idempotency
- Line
- .UpdateSupplier
- Run
- newUseCase
- MockLogoFiles
- .Return
- billing/usecase/repository.go
- Consumer
- PayableUseCase
- .Return
- .Return
- IdentityUseCase
- StaffUseCase
- support/repository/postgres_integration_test.go
- Mailpit
- Tx
- PurchaseOrder
- newEnv
- profit.go
- ReconciliationData
- newEnv
- Deps
- Status
- checkedFile
- billing/handler/branches_test.go
- MockInventoryDirectory
- UseCase
- env
- File
- MockEntitlementCache
- DocumentUseCase
- CustomerUseCase
- ExpiryJob
- MigrationsTestSuite
- UseCase
- NewTechnicianUseCase
- PaymentOutInfo
- MockSupplierRepository
- GoodsReceiptStatus
- .verifier
- Line
- .buildLine
- newFixture
- db_tracer_test.go
- .create
- newWorld
- Deps
- importRequest
- Sheet
- contactrequest/usecase/usecase_test.go
- .CreateStockIssue
- authorizeGarage
- MockCatalogueFiles_Expecter
- MockCatalogueUsageChecker_Expecter
- MockCustomerUsageChecker_CustomerInUse_Call
- .GetProfitReport
- buildMIME
- VietGara Backend
- contactrequest/handler/http_test.go
- PurchaseLine
- MockTransactionCounter
- MockUsageReader
- toAppError
- RepairOrderStatus
- Verifier
- assertAppError
- toAppError
- Reminder
- StockMovementRow
- .sendOTPFor
- Kind
- GarageInfo
- RepairOrderInfo
- newEnv
- OrderMail
- MockNotificationGarageReader
- TestConsumer
- Load test (Release 9.0, NFR-PF-06)
- HistoryEntry
- Deps
- MockNotificationReceipts_PurchaseOrder_Call
- MockNotificationSuppressions_Suppress_Call
- MockRepairOrderStockIssues_Posted_Call
- MockReportingNameReader_JobNames_Call
- numbering.go
- Blackbox tests
- UserSummary
- .EnrollMFA
- GarageSettings
- results/README.md
- Database Migrations & Row-Level Security (RLS)
- SettlementStatus
- sampled
- DocumentNumbers
- MockBillingEventPublisher
- partquantity.go
- PaymentMethod
- google.golang.org/grpc/metadata.MD
- GoogleOAuthConfig
- newEnv
- MemoryTransactionCounter
- internalError
- logHandler
- QuoteReady
- QueryTracer
- Membership
- GarageInfo
- line
- Deps
- MockBillingTransactionLimiter_ConsumeTransaction_Call
- MockDataRequestTransactor_InGarage_Call
- MockFileEntityChecker_EntityExists_Call
- MockFileUsageChecker_FileInUse_Call
- MockGarageOwnerReader_GarageOwner_Call
- MockIdentityEventPublisher_Publish_Call
- MockImportSuppliers_IDsByName_Call
- MockInventoryEventPublisher_Publish_Call
- MockInventorySequencer_Next_Call
- MockReceivableGarageReader_Timezone_Call
- MockReceivableSequencer_Next_Call
- MockRepairOrderEventPublisher_Publish_Call
- MockRepairOrderSequencer_Next_Call
- MockRepairOrderTransactionLimiter_ConsumeTransaction_Call
- MockRow_Scan_Call
- MockSupplierUsageChecker_SupplierInUse_Call
- MockTransactionLimiter_ConsumeTransaction_Call
- MockVehicleReader_Find_Call
- .List
- PageRequest
- E-mail
- .Get
- observability/request.go
- MockLicenseEventPublisher_Publish_Call
- MockLicenseUsageReader_OwnerUsage_Call
- MockQuoteEventPublisher_Publish_Call
- MockReceivableStaffReader_FullName_Call
- MockReceivableTransactor_InGarage_Call
- MockRepairOrderVehicleReader_Find_Call
- MockSequencer_Next_Call
- MockTenantEventPublisher_Publish_Call
- PageRequest
- github.com/viettechno/vietgara-backend
- github.com/viettechno/vietgara-backend/testing/blackbox

## God Nodes (most connected - your core abstractions)
1. `QuerierFromContext()` - 276 edges
2. `call()` - 274 edges
3. `ToGRPCError()` - 173 edges
4. `RequireAPIError()` - 172 edges
5. `garagePath()` - 149 edges
6. `New()` - 138 edges
7. `Pool` - 117 edges
8. `NewVerifiedOwner()` - 108 edges
9. `Row()` - 96 edges
10. `build()` - 92 edges

## Surprising Connections (you probably didn't know these)
- `Observability` --references--> `quote()`  [INFERRED]
  README.md → internal/document/render/pdf_test.go
- `Database Migrations & Row-Level Security (RLS)` --references--> `public()`  [INFERRED]
  db/README.md → internal/quote/handler/grpc_handler.go
- `Row-Level Security` --references--> `TestRLS_TenantModel()`  [INFERRED]
  db/README.md → internal/tenant/repository/repository_integration_test.go
- `Security checks` --references--> `TestEveryRPCDeclaresItsAuthorization()`  [INFERRED]
  README.md → cmd/server/policies_test.go
- `Events` --references--> `events`  [INFERRED]
  README.md → internal/identity/handler/helpers_test.go

## Import Cycles
- 4-file cycle: `internal/license/usecase/admin.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go -> internal/license/usecase/admin.go`
- 4-file cycle: `internal/license/usecase/admin.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go -> internal/license/usecase/admin.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/entitlement.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/entitlement.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/expiry_job.go -> internal/platform/db/pool_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/expiry_job.go -> internal/platform/db/transaction_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/payment.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/payment.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/ports.go -> internal/platform/db/pool_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/ports.go -> internal/platform/db/transaction_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/review.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/review.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/subscription.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/subscription.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/sync.go -> internal/platform/db/pool_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/sync.go -> internal/platform/db/transaction_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/usecase_test.go -> internal/middleware/support_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/license/usecase/credit_test.go -> internal/license/usecase/usecase_test.go -> internal/middleware/tenant_test.go -> internal/mocks/license_usecase_subscription_repository.go -> internal/license/usecase/credit_test.go`
- 4-file cycle: `internal/mocks/quote_usecase_job_catalogue.go -> internal/quote/usecase/billing_party_test.go -> internal/quote/usecase/decision.go -> internal/platform/db/pool_test.go -> internal/mocks/quote_usecase_job_catalogue.go`
- 4-file cycle: `internal/mocks/quote_usecase_job_catalogue.go -> internal/quote/usecase/billing_party_test.go -> internal/quote/usecase/decision.go -> internal/platform/db/transaction_test.go -> internal/mocks/quote_usecase_job_catalogue.go`

## Communities (293 total, 42 thin omitted)

### Community 0 - "context.Context"
Cohesion: 0.01
Nodes (100): envOr(), newFileStore(), importPartInfo(), RLSContext, linkCodes, linkDirectory, linkGarage, notBilled (+92 more)

### Community 1 - "ToGRPCError"
Cohesion: 0.01
Nodes (55): Server, toInt32(), typeFromProto(), typeToProto(), Server, excelBody(), pdfBody(), Server (+47 more)

### Community 2 - "go_pkg_context"
Cohesion: 0.15
Nodes (37): FieldTooLongError, ValidTaxID(), normalizeTaxID(), StockCountRow, PurchasesExcel(), sheet(), StockCountExcel(), StockValuationExcel() (+29 more)

### Community 3 - "call"
Cohesion: 0.01
Nodes (24): ReceivableFilter, catalogue, supportCase, team, receiptLine, call(), garagePath(), categoryPath() (+16 more)

### Community 4 - "Pool"
Cohesion: 0.02
Nodes (227): buildBilling(), buildCatalogue(), TestCatalogueModule(), buildContactRequests(), registerPublicRoutes(), buildCustomerLinks(), TestCustomerUserLinkPersistence(), buildDataImport() (+219 more)

### Community 5 - "go_pkg_testing"
Cohesion: 0.08
Nodes (75): APIError, Target, Minted, Read(), TestReadKeepsSheetRowNumbersAndSkipsBlankRows(), TestReadRejectsWhatIsNotAWorkbook(), TestReadReturnsTextAsTyped(), TestReadStopsAfterMoreRowsThanAnImportAllows() (+67 more)

### Community 6 - "testing.T"
Cohesion: 0.02
Nodes (238): TestEveryRPCDeclaresItsAuthorization(), dsrKit, TestMigrationsSuite(), ToAppError(), TestToAppError(), TestToAppError_InactiveJob(), TestCustomerConsent(), TestEmailOptOut() (+230 more)

### Community 7 - "TimeToProto"
Cohesion: 0.01
Nodes (57): PurchaseOrderServer, run(), paymentToProto(), settlementToProto(), timeToProtoPtr(), Server, requestToProto(), WriteOffReason (+49 more)

### Community 8 - "github.com/stretchr/testify/mock.Call"
Cohesion: 0.01
Nodes (49): MockDataRequestNames_Expecter, MockDataRequestNames_GarageName_Call, MockDataRequestNames_UserNames_Call, MockInventoryRepository_Balances_Call, MockInventoryRepository_ClearStockCountLines_Call, MockInventoryRepository_CreateGoodsReceipt_Call, MockInventoryRepository_CreatePartReturn_Call, MockInventoryRepository_CreatePurchaseOrder_Call (+41 more)

### Community 9 - "Part"
Cohesion: 0.02
Nodes (74): Image, CheckLength(), Category, PackagingUnit, Part, PartBarcode, Unit, NormalizeBarcode() (+66 more)

### Community 10 - "NewVerifiedOwner"
Cohesion: 0.02
Nodes (197): publicKit, Authenticator, localeToProto(), GoogleStub, NewGoogleStub(), OptOutLinkToken(), Get(), RequireSlow() (+189 more)

### Community 11 - "New"
Cohesion: 0.03
Nodes (204): NewPostgresRepository(), newSettlement(), paymentRow(), settlementRow(), TestPostgresRepository_BilledToTaxProfile(), TestPostgresRepository_Create(), TestPostgresRepository_FindByID(), TestPostgresRepository_FindByRepairOrderID() (+196 more)

### Community 12 - "UserID"
Cohesion: 0.02
Nodes (44): VehicleToProto(), garageLinkToProto(), Server, NewServer(), Server, ClaimsFromContext(), TestClaimsHelpers(), UserID() (+36 more)

### Community 13 - "github.com/stretchr/testify/mock.Mock"
Cohesion: 0.01
Nodes (98): TestFileEntities(), NewMockCatalogueFiles(), NewMockCatalogueRepository(), NewMockCatalogueUsageChecker(), NewMockHistoryReader(), NewMockImportReader(), NewMockIdentityEventPublisher(), NewMockMailer() (+90 more)

### Community 14 - "Repo"
Cohesion: 0.02
Nodes (52): normalizeNames(), TestPlatformAccessCheck(), TestPlatformCatalogue(), AllPlatform(), PlatformAccess, PlatformPermission, PlatformReader, NormalizePlatform() (+44 more)

### Community 15 - ".Return"
Cohesion: 0.02
Nodes (39): ImageRef, JobFilter, MockCatalogueRepository_AddImage_Call, MockCatalogueRepository_BarcodesByCode_Call, MockCatalogueRepository_CategoryHasParts_Call, MockCatalogueRepository_CreateCategory_Call, MockCatalogueRepository_CreateJob_Call, MockCatalogueRepository_CreatePart_Call (+31 more)

### Community 16 - "QuerierFromContext"
Cohesion: 0.02
Nodes (51): consentJSON(), compactPattern(), containsPattern(), execAffectingOne(), findOne(), isUUID(), listPage(), nullable() (+43 more)

### Community 17 - "server/main.go"
Cohesion: 0.09
Nodes (21): accessKey, platformAccessKey, jwks, tokenClaims, requestBody, ErrorBody, ErrorDetail, ErrorPayload (+13 more)

### Community 18 - "RequireAPIError"
Cohesion: 0.04
Nodes (130): row, orderKit, Client, NewPart(), NewUnit(), WithBrand(), WithCategory(), WithPrice() (+122 more)

### Community 19 - "go_pkg_time"
Cohesion: 0.13
Nodes (4): afterCommitKey, scopeKey, TxKey, nullableText

### Community 20 - "run"
Cohesion: 0.02
Nodes (33): badMask(), barcodeToProto(), categoryToProto(), deleteOutcome(), Server, NewServer(), pageRequest(), paginationToProto() (+25 more)

### Community 21 - "Payable"
Cohesion: 0.03
Nodes (39): MockReceivablePayableRepository, Allocation, Payable, PaymentOutVoucher, PostgresRepository, scanPayable(), scanVoucher(), payableAppError() (+31 more)

### Community 22 - "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1.Session"
Cohesion: 0.03
Nodes (43): Options, Owner, Population, clientAPI, fakeAPI, fakePaymentRequest, fakeState, Summary (+35 more)

### Community 23 - "newUseCase"
Cohesion: 0.06
Nodes (98): expectOut(), expectStock(), postedIssue(), resolved(), TestInventoryUseCase_CreateSupplierReturn(), TestInventoryUseCase_CreateWriteOff(), TestInventoryUseCase_ListRepairOrderPartReturns(), TestInventoryUseCase_ListTransactionsNamesTheUser() (+90 more)

### Community 24 - "toAppError"
Cohesion: 0.04
Nodes (33): TestCostPerBaseUnit(), GoodsReceipt, GoodsReceiptLine, CostPerBaseUnit(), receiptLinesFromProto(), newReceipt(), actorRef(), toAppError() (+25 more)

### Community 25 - "run"
Cohesion: 0.03
Nodes (19): TaxProfileServer, Server, badMask(), maskPaths(), run(), stringField(), LinkStatusToProto(), LinkToProto() (+11 more)

### Community 26 - "Plan"
Cohesion: 0.04
Nodes (44): InvalidPlanError, activeSubscription(), proPlan(), TestApplyPayment(), TestChangePlanAndExtend(), TestCountsTransaction(), TestEntitlements(), TestNewTrialLastsTheTrialPlansDays() (+36 more)

### Community 27 - "newReportEnv"
Cohesion: 0.03
Nodes (57): buildReporting(), profitLines(), reportEnv, MockReportingCostReader, NewMockReportingCostReader(), MockReportingGarageReader, NewMockReportingGarageReader(), MockReportingNameReader (+49 more)

### Community 28 - "RedisStore"
Cohesion: 0.04
Nodes (28): OTP, googleLinkKey(), mfaChallengeKey(), mfaFailuresKey(), NewMemoryStore(), NewRedisStore(), otpKey(), refreshKey() (+20 more)

### Community 29 - "Receivable"
Cohesion: 0.04
Nodes (21): MockReceivableRepository, Entry, EntryKind, Line, Outcome, Receivable, Reminder, outcomeToProto() (+13 more)

### Community 30 - ".Return"
Cohesion: 0.03
Nodes (19): MissingSettlement, MockReceivableRepository_AddEntries_Call, MockReceivableRepository_AnonymizeCustomer_Call, MockReceivableRepository_BalanceBefore_Call, MockReceivableRepository_Entries_Call, MockReceivableRepository_Expecter, MockReceivableRepository_FindByID_Call, MockReceivableRepository_FindBySettlement_Call (+11 more)

### Community 31 - "apperror.go"
Cohesion: 0.24
Nodes (3): TestEmailVerification(), hostnameChars(), valid()

### Community 32 - "time.Time"
Cohesion: 0.04
Nodes (33): purgeTestUsers(), TestPurgeTestUsersDeletesMailAndFilesBeforeUsersWithSameCutoff(), TestPurgeTestUsersDoesNotContactMailpitWhenListingFails(), TestPurgeTestUsersDoesNotDeleteAnythingWhenGarageListingFails(), TestPurgeTestUsersDoesNotDeleteUsersWhenFileCleanupFails(), TestPurgeTestUsersDoesNotDeleteUsersWhenMailpitFails(), RevenueRow, RevenueExcel() (+25 more)

### Community 33 - "newEnv"
Cohesion: 0.03
Nodes (49): defaultGarage(), env, newEnv(), TestExportInventoryDocuments(), TestExportPaymentVoucherPdf(), TestExportQuotePdf(), TestExportReceivableReconciliationExcel(), TestExportReceivablesExcel() (+41 more)

### Community 34 - "jobs_integration_test.go"
Cohesion: 0.53
Nodes (6): countLine(), TestStockCount_Summarise(), TestStockCountLine(), orZero(), SuggestedQuantity(), TestSuggestedQuantity()

### Community 35 - "Scope"
Cohesion: 0.03
Nodes (29): ConnPool, Querier, passTx, passTx, passTx, Transactor, Transactor, InTransaction() (+21 more)

### Community 36 - "Server"
Cohesion: 0.03
Nodes (15): PlatformCatalogEntry, Server, NewServer(), groupFields(), groupToProto(), invitationStatusFromProto(), invitationsToProto(), invitationToProto() (+7 more)

### Community 37 - "Subscription"
Cohesion: 0.04
Nodes (19): Subscription, SubscriptionStatus, PostgresRepository, scanSubscription(), SubscriptionFilter, SubscriptionPage, subscriptionAudit(), NewMockSubscriptionRepository() (+11 more)

### Community 38 - "Entitlements"
Cohesion: 0.05
Nodes (30): Enforcement, Entitlements, LimitType, Server, NewServer(), CacheKey(), MemoryEntitlementCache, NewRedisEntitlementCache() (+22 more)

### Community 39 - ".Return"
Cohesion: 0.04
Nodes (13): MockTx, MockTx_Begin_Call, MockTx_Commit_Call, MockTx_Conn_Call, MockTx_CopyFrom_Call, MockTx_Exec_Call, MockTx_Expecter, MockTx_LargeObjects_Call (+5 more)

### Community 40 - "UserLink"
Cohesion: 0.05
Nodes (23): fake, LinkServer, oneLink, LinkChannel, LinkStatus, UserLink, linkChannelToProto(), NewLinkServer() (+15 more)

### Community 41 - ".Return"
Cohesion: 0.04
Nodes (16): MockSessionStore_ClearMFAFailures_Call, MockSessionStore_ConsumeGoogleLinkProof_Call, MockSessionStore_ConsumeRefreshToken_Call, MockSessionStore_ConsumeResetToken_Call, MockSessionStore_DeleteMFAChallenge_Call, MockSessionStore_Expecter, MockSessionStore_MFAChallengeUser_Call, MockSessionStore_MFAFailures_Call (+8 more)

### Community 42 - "apiPolicies"
Cohesion: 0.07
Nodes (76): apiPolicies(), TestEveryPlatformPermissionGuardsSomething(), Policies(), Policies(), TestPolicies(), JobPolicies(), TestJobPolicies(), TestPolicies() (+68 more)

### Community 43 - "NewBusiness"
Cohesion: 0.05
Nodes (63): payables, operation, session, AsAPIError(), NewBusiness(), PDFText(), Writing a scenario, company() (+55 more)

### Community 44 - "WithClaims"
Cohesion: 0.05
Nodes (67): fixture, server(), signedIn(), TestErrors(), TestGarageLinks(), TestGetMyVehicleHistory(), TestListMyVehicles(), WithClaims() (+59 more)

### Community 45 - "Vehicle"
Cohesion: 0.05
Nodes (38): vehicles, CheckLength(), TestCheckLength(), TestNormalizeLicensePlate(), TestValidateYear(), IdentityField, Vehicle, LicensePlateKey() (+30 more)

### Community 46 - "toAppError"
Cohesion: 0.07
Nodes (21): CreditExpiry(), PaymentInstructions, PaymentSettings, PaymentUseCase, RequestView, requestAudit(), isPaymentError(), paymentAppError() (+13 more)

### Community 47 - "Quote"
Cohesion: 0.05
Nodes (20): MockQuoteRepository, NewMockQuoteRepository(), Quote, scanQuote(), nullableUUID(), decideConsumables(), LineDecisionInput, addJob() (+12 more)

### Community 48 - "preview.go"
Cohesion: 0.09
Nodes (41): NormalizeHeader(), TestNormalizeHeader(), permissions(), finish(), UseCase, newHeaderCheck(), parseFactor(), partOfRow() (+33 more)

### Community 49 - "Internal"
Cohesion: 0.07
Nodes (31): toAppError(), HashRecoveryCode(), NewRecoveryCodes(), NormalizeRecoveryCode(), TestRecoveryCodes(), googleLinkError(), Session, invalidCredentials() (+23 more)

### Community 50 - "wiring.go"
Cohesion: 0.15
Nodes (17): ConvertToBase(), DerivedPrice(), FormatDecimal(), NormalizeFactor(), NormalizeThreshold(), ParseDecimal(), TestConvertToBase(), TestDerivedPrice() (+9 more)

### Community 51 - ".FindByID"
Cohesion: 0.04
Nodes (29): allPages(), deref(), firstNonEmpty(), issuerOf(), nonEmpty(), SupplierView, MockReceivableSupplierReader_Get_Call, billingBillingParties (+21 more)

### Community 52 - "assertAppError"
Cohesion: 0.07
Nodes (52): env, TestLinkRPCs(), EventPublisher, TestCustomerUseCase_Anonymize(), TestCustomerUseCase_AnonymizedCustomerCannotBeChanged(), TestCustomerUseCase_Export(), TestCustomerUseCase_OptOutEmailOfAnAnonymizedCustomerChangesNothing(), WithEvents() (+44 more)

### Community 53 - "Customer"
Cohesion: 0.05
Nodes (18): ConsentRecord, Customer, scanCustomer(), lookupContacts(), matchesContact(), ListFilter, MockCustomerRepository, MockCustomerRepository_Anonymize_Call (+10 more)

### Community 54 - "newUseCase"
Cohesion: 0.06
Nodes (49): MockPartQuantityResolver, MockRepairOrderEventPublisher, NewMockRepairOrderEventPublisher(), MockRepairOrderGarageReader, NewMockRepairOrderGarageReader(), NewMockRepairOrderRepository(), MockRepairOrderSequencer, NewMockRepairOrderSequencer() (+41 more)

### Community 55 - "StaffGroup"
Cohesion: 0.05
Nodes (15): DefaultRole, DefaultRoleTemplate, NewMockGroupRepository(), StaffGroup, scanGroup(), MockGroupRepository, MockGroupRepository_Create_Call, MockGroupRepository_CreateDefault_Call (+7 more)

### Community 56 - "TaxProfile"
Cohesion: 0.06
Nodes (28): memProfiles, TaxProfile, TaxProfileKind, NewTaxProfileServer(), taxProfileKindFromProto(), taxProfileKindToProto(), TestTaxProfileRPCs(), scanTaxProfile() (+20 more)

### Community 57 - "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1.RepairOrder"
Cohesion: 0.07
Nodes (30): kit, reportKit, repairOrderPath(), within(), newVehicle(), TestFilterByTechnician(), TestRepairOrderNumberFromTemplate(), adhocOrder() (+22 more)

### Community 58 - ".Return"
Cohesion: 0.05
Nodes (12): MockRows, NewMockRows(), MockRows_Close_Call, MockRows_CommandTag_Call, MockRows_Conn_Call, MockRows_Err_Call, MockRows_Expecter, MockRows_FieldDescriptions_Call (+4 more)

### Community 59 - "Role"
Cohesion: 0.06
Nodes (13): NewMockRoleRepository(), Role, scanRole(), MockRoleRepository, MockRoleRepository_Create_Call, MockRoleRepository_CreateDefault_Call, MockRoleRepository_Delete_Call, MockRoleRepository_Expecter (+5 more)

### Community 60 - "Entry"
Cohesion: 0.04
Nodes (22): linkAudit, AuditLogger, AuditLogger, AuditLogger, AuditLogger, Entry, toJSON(), AuditLogger (+14 more)

### Community 61 - ".Return"
Cohesion: 0.05
Nodes (17): orderDay(), PurchaseOrderRow, StockIssueRow, PurchaseRow, ValuationRow, GoodsReceiptInfo, PurchaseOrderInfo, StockCountInfo (+9 more)

### Community 62 - "github.com/jackc/pgx/v5/pgconn.CommandTag"
Cohesion: 0.06
Nodes (13): MockConnPool, NewMockQuerier(), MockConnPool_Begin_Call, MockConnPool_Close_Call, MockConnPool_Exec_Call, MockConnPool_Expecter, MockConnPool_Query_Call, MockConnPool_QueryRow_Call (+5 more)

### Community 63 - "preview_test.go"
Cohesion: 0.13
Nodes (55): confirmable(), TestConfirmAlreadyImported(), TestConfirmFailuresAfterTheChecks(), TestConfirmGoodsReceiptsApplies(), TestConfirmInvalidFile(), TestConfirmPartsApplies(), TestConfirmPartsBindsPackagingBarcodeToItsUnit(), TestConfirmPermissionRequired() (+47 more)

### Community 64 - "UserSummary"
Cohesion: 0.06
Nodes (22): summaryOrNotFound(), toSummary(), NewMockUserDirectory(), UserDirectory, UserSummary, MockUserDirectory, MockUserDirectory_Expecter, MockUserDirectory_FindByEmail_Call (+14 more)

### Community 65 - "Garage"
Cohesion: 0.06
Nodes (21): NewMockGarageRepository(), TestStatuses(), BankDetails, Garage, GarageStatus, QuoteApprovalMode, StockIssueMode, garageStatusFromProto() (+13 more)

### Community 66 - "newEnv"
Cohesion: 0.09
Nodes (49): TestMappings(), TestPartReturnHandlers(), TestSupplierReturnHandlers(), TestWriteOffHandlers(), env, newEnv(), samplePart(), signedIn() (+41 more)

### Community 67 - "newUseCase"
Cohesion: 0.09
Nodes (53): cartonPart(), TestCreateParts(), TestLookupPartByBarcode(), TestPartsBySKUsAndBarcodes(), TestSetPartBarcodes(), TestSetPartBarcodesFailures(), TestSetPartBarcodesLimits(), TestSetPartBarcodesTrimsCodes() (+45 more)

### Community 68 - ".Return"
Cohesion: 0.06
Nodes (13): CategoryInfo, UnitInfo, MockImportCatalogue, NewMockImportCatalogue(), MockImportCatalogue_Categories_Call, MockImportCatalogue_CreateCategory_Call, MockImportCatalogue_CreateParts_Call, MockImportCatalogue_CreateUnit_Call (+5 more)

### Community 69 - "newPool"
Cohesion: 0.11
Nodes (49): TestTaxProfilePersistence(), TestPostgresCustomerRepository_Anonymize(), TestPostgresCustomerRepository_Anonymize_Failures(), NewPostgresCustomerRepository(), customerValues(), TestPostgresCustomerRepository_Create(), TestPostgresCustomerRepository_FindByEmailAcrossGarages(), TestPostgresCustomerRepository_FindByID() (+41 more)

### Community 70 - "sync_test.go"
Cohesion: 0.27
Nodes (15): TestAnonymizedPlaceholdersAreInStep(), TestPayable_DeriveRemainingAndAge(), TestPaymentOutVoucher_Amounts(), TestDueOffsets(), TestEntryBalance(), TestReceivableAmountsAndStatus(), offsetsFromProto(), Sequencer (+7 more)

### Community 71 - "newPayableEnv"
Cohesion: 0.06
Nodes (39): payableEnv, MockReceivableAuditLogger, NewMockReceivableAuditLogger(), MockReceivableGarageReader, NewMockReceivableGarageReader(), MockReceivablePaidOutReader, NewMockReceivablePaidOutReader(), NewMockReceivablePayableRepository() (+31 more)

### Community 72 - "Invitation"
Cohesion: 0.07
Nodes (8): NewMockInvitationRepository(), Invitation, InvitationStatus, scanInvitation(), MockInvitationRepository, PostgresInvitationRepository, InvitationRepo, InvitationRepository

### Community 73 - "Log"
Cohesion: 0.07
Nodes (13): Channel, MockNotificationRepository, Log, Status, LogFilter, Repository, MockNotificationRepository_AlreadySent_Call, MockNotificationRepository_ClearRecipients_Call (+5 more)

### Community 74 - "DocumentType"
Cohesion: 0.10
Nodes (27): fakePrints, printRecord, TestEveryDocumentTypeIsMappedToProto(), TestPrintSettingsRPCs(), DocumentType, Paper, TestDocumentType(), TestFixtureCoversEveryType() (+19 more)

### Community 75 - "NewPool"
Cohesion: 0.05
Nodes (38): confirmedDomain(), execute(), main(), newDatabasePool(), run(), TestConfirmedDomain(), main(), NewTestUserPurgeJob() (+30 more)

### Community 76 - "IsZero"
Cohesion: 0.06
Nodes (31): NormalizeReason(), TestNormalizeReason(), TestDecimalHelpers(), TestParseDate(), TestStockValueAndRounding(), Add(), StockValuationCategory, StockValuationLine (+23 more)

### Community 77 - "newEnv"
Cohesion: 0.08
Nodes (38): roleServer, TestBarcodeRPCFailures(), TestBarcodeRPCs(), code(), mask(), newEnv(), samplePart(), sampleUnit() (+30 more)

### Community 78 - "As"
Cohesion: 0.09
Nodes (42): AppError, FieldViolation, reasonOfError(), isAppError(), NewMockPartQuantityResolver(), TestQuoteUseCase_Create_BillingParty(), TestQuoteUseCase_Update_BillingParty(), approvedQuote() (+34 more)

### Community 79 - "Settlement"
Cohesion: 0.08
Nodes (15): Payment, Settlement, PostgresRepository, isUniqueViolation(), nullableString(), scanPayment(), scanSettlement(), paymentAudit() (+7 more)

### Community 80 - "SettlementView"
Cohesion: 0.06
Nodes (25): mustLocation(), MockReceivableSettlementReader, NewMockReceivableSettlementReader(), DueDateFor(), MethodTotal, Reconciliation, ParsePeriod(), day() (+17 more)

### Community 81 - "Message"
Cohesion: 0.06
Nodes (17): mailer, fakeMailer, Mailer, mailer, fakeMailer, Mailer, Mailer, Message (+9 more)

### Community 82 - "Party"
Cohesion: 0.07
Nodes (20): Server, NewServer(), actorRef(), isAny(), toAppError(), firstNonBlank(), BillingPartyResolver, checkInvoice() (+12 more)

### Community 83 - "Settings"
Cohesion: 0.08
Nodes (16): Audience, MockNotificationSettings, NewMockNotificationSettings(), SkipReason, Settings, Type, TestType(), failureReason() (+8 more)

### Community 84 - "PlanPaymentRequest"
Cohesion: 0.08
Nodes (17): TestPaymentKindAndStatus(), PaymentKind, PaymentMethod, PaymentStatus, PlanPaymentRequest, paymentMethodFromProto(), PostgresRepository, nullableString() (+9 more)

### Community 85 - "setup"
Cohesion: 0.13
Nodes (43): jobPartInputs(), garageKit, eventually(), hidden(), inUnit(), newJob(), setup(), sku() (+35 more)

### Community 86 - "quoteToProto"
Cohesion: 0.06
Nodes (17): BillingPartyToProto(), BillingTaxProfileID(), TestBillingPartyToProto(), TestBillingTaxProfileID(), Server, run(), approvalModeFromProto(), approvalModeToProto() (+9 more)

### Community 87 - "Access"
Cohesion: 0.08
Nodes (24): CatalogEntry, All(), FromContext(), Access, Permission, Normalize(), ReadOnly(), TestCatalog() (+16 more)

### Community 88 - "ReceivableUseCase"
Cohesion: 0.07
Nodes (20): bus, DefaultSettings(), Settings, TestSettingsNormalize(), Server, NewServer(), TestSubscribe(), Deps (+12 more)

### Community 89 - "LocalStore"
Cohesion: 0.09
Nodes (17): deletePrefix(), TestGCSStoreSignedURL(), NewGCS(), newGCSStore(), NewGCSWithPrivate(), removeTree(), cleanKey(), cleanPrefix() (+9 more)

### Community 90 - ".Return"
Cohesion: 0.07
Nodes (10): VehicleFilter, MockVehicleRepository_Create_Call, MockVehicleRepository_Expecter, MockVehicleRepository_FindByID_Call, MockVehicleRepository_IDsByCustomer_Call, MockVehicleRepository_List_Call, MockVehicleRepository_SetActive_Call, MockVehicleRepository_SoftDelete_Call (+2 more)

### Community 91 - "check_traceability.py"
Cohesion: 0.09
Nodes (19): api_coverage(), _balanced(), called_methods(), client_table(), collect_calls(), documented_from_dir(), documented_from_ref(), load_snapshot() (+11 more)

### Community 92 - "net/http.ResponseWriter"
Cohesion: 0.10
Nodes (22): bodyLimit(), mailjetEvent, deleteWriter, Server, Server, Server, notFound(), optOutAction() (+14 more)

### Community 93 - "Store"
Cohesion: 0.10
Nodes (3): CreditEntryKind, CreditEntry, Store

### Community 94 - "Request"
Cohesion: 0.08
Nodes (14): Request, Source, Status, sourceToProto(), statusFromProto(), statusToProto(), PostgresRepository, isUUID() (+6 more)

### Community 95 - "newEnv"
Cohesion: 0.08
Nodes (33): userServer, TestSignInWithGoogleIdToken_CreatesAVerifiedUser(), TestSignInWithGoogleIdToken_Errors(), TestCompleteGoogleLinkRPC(), TestLinkAndUnlinkGoogleRPCs(), TestLinkGoogleNeedsAVerifiableToken(), TestLinkPopupErrors(), verifiedUser() (+25 more)

### Community 96 - "User"
Cohesion: 0.09
Nodes (7): users, User, newFakeUsers(), NewMockUserRepository(), MockUserRepository, fakeUsers, UserRepository

### Community 97 - ".Return"
Cohesion: 0.07
Nodes (9): MockRepairOrderRepository_Create_Call, MockRepairOrderRepository_ExistsForVehicles_Call, MockRepairOrderRepository_Expecter, MockRepairOrderRepository_FindByID_Call, MockRepairOrderRepository_ListForVehicle_Call, MockRepairOrderRepository_LiveVehicleIDs_Call, MockRepairOrderRepository_Lock_Call, MockRepairOrderRepository_ReplaceLines_Call (+1 more)

### Community 98 - "Validation"
Cohesion: 0.09
Nodes (34): hasControl(), singleLine(), spoofing(), validEmail(), validPhone(), Locale, NormalizeFullName(), TestNamesAndPasswords() (+26 more)

### Community 99 - "newPublicLimit"
Cohesion: 0.08
Nodes (30): publicEnv, NewServer(), env, newEnv(), newPublicEnv(), signedIn(), TestAViewIsAnsweredOnceAndNeverStored(), TestCreateListGetComplete() (+22 more)

### Community 100 - ".Return"
Cohesion: 0.07
Nodes (9): ListFilter, MockBillingRepository_Create_Call, MockBillingRepository_Expecter, MockBillingRepository_FindByID_Call, MockBillingRepository_FindByRepairOrderID_Call, MockBillingRepository_List_Call, MockBillingRepository_SaveBilling_Call, MockBillingRepository_SaveStatus_Call (+1 more)

### Community 101 - "newUseCase"
Cohesion: 0.11
Nodes (39): TestReceivableUseCase_HandleCustomerAnonymized(), TestReceivableUseCase_SyncFollowsTheLiveCustomer(), TestReceivableUseCase_SyncKeepsAnAnonymizedName(), assertAppError(), day(), newUseCase(), openReceivable(), newPayableUseCase() (+31 more)

### Community 102 - "repairorder.go"
Cohesion: 0.32
Nodes (12): TestSupplierUsage(), TestRepairOrder_DeriveStatus(), TestRepairOrder_PartLines(), TestRepairOrder_ReadyToClose(), TestRepairOrder_Recalculate(), paginationToProto(), toInt32(), lineRow() (+4 more)

### Community 103 - "AdjustmentLine"
Cohesion: 0.06
Nodes (18): CreditPerEnteredUnit(), AdjustmentLine, TestCreditPerEnteredUnit(), TestValueDong(), TestLineAmount(), LineAmount(), roundHalfUp(), ValueDong() (+10 more)

### Community 104 - "QuoteUseCase"
Cohesion: 0.09
Nodes (20): TestApprovalMode(), TestCheckLength(), TestLineTotal(), CheckLength(), ApprovalMode, LineTotal(), Server, NewServer() (+12 more)

### Community 105 - "RepairOrder"
Cohesion: 0.07
Nodes (9): repairOrders, MockRepairOrderRepository, RepairOrder, nullableString(), nullableUUID(), ListFilter, Repository, repairOrders (+1 more)

### Community 106 - "PartInfo"
Cohesion: 0.08
Nodes (12): partInfos(), PartInfo, ResolvedQuantity, MockInventoryPartCatalogue, NewMockInventoryPartCatalogue(), MockInventoryPartCatalogue_Expecter, MockInventoryPartCatalogue_Parts_Call, MockInventoryPartCatalogue_ResolveQuantity_Call (+4 more)

### Community 107 - "PublicUseCase"
Cohesion: 0.09
Nodes (15): Confirmation, PublicServer, NewPublicServer(), PublicDeps, PublicUseCase, hashToken(), NewPublicUseCase(), newToken() (+7 more)

### Community 108 - "PortalUseCase"
Cohesion: 0.10
Nodes (15): garages, Customers, Deps, GarageInfo, GarageLink, Garages, PortalUseCase, Transactor (+7 more)

### Community 109 - "rls_test.go"
Cohesion: 0.10
Nodes (16): NewRLSContext(), newRLSTestDB(), rlsSessionStep(), TestNewRLSContext(), TestRLSContext_AssignStaffToGarage(), TestRLSContext_GetGarage(), TestRLSContext_GetGarageStaff(), TestRLSContext_RemoveStaffFromGarage() (+8 more)

### Community 110 - ".Return"
Cohesion: 0.08
Nodes (9): Counts, CustomerRef, Customers, MockDataRequestCustomers_Anonymize_Call, MockDataRequestCustomers_ByEmail_Call, MockDataRequestCustomers_Check_Call, MockDataRequestCustomers_Expecter, MockDataRequestCustomers_Export_Call (+1 more)

### Community 111 - "Garage"
Cohesion: 0.12
Nodes (32): Auth(), EmailVerified(), policyFromContext(), chain(), TestAuthPipeline(), TestInterceptorsWithoutAuth(), info(), okHandler() (+24 more)

### Community 112 - "InventoryUseCase"
Cohesion: 0.10
Nodes (17): TestIsLow(), StockLevel, IsLow(), Server, NewServer(), groupLowStock(), PartQuery, Dependencies (+9 more)

### Community 113 - "PostgresUserRepository"
Cohesion: 0.08
Nodes (10): NameSource, AuthProvider, UserStatus, TestEnums(), userStatusToProto(), mapUniqueViolation(), scanUser(), foreignKeyViolation() (+2 more)

### Community 114 - "pdf.go"
Cohesion: 0.17
Nodes (26): goodsReceiptTemplate(), join(), stockIssueTemplate(), TestVoucherTextFitsItsCells(), paymentOutTemplate(), drawCopyMark(), drawLogo(), finish() (+18 more)

### Community 115 - "Staff"
Cohesion: 0.10
Nodes (6): NewMockStaffRepository(), Staff, MockStaffRepository, StaffRepo, Store, StaffRepository

### Community 116 - ".Return"
Cohesion: 0.08
Nodes (8): MockPlanRepository_CountSubscribers_Call, MockPlanRepository_Create_Call, MockPlanRepository_Expecter, MockPlanRepository_FindByCode_Call, MockPlanRepository_FindByID_Call, MockPlanRepository_ListAll_Call, MockPlanRepository_ListPublic_Call, MockPlanRepository_Update_Call

### Community 117 - ".Return"
Cohesion: 0.08
Nodes (8): MockStaffRepository_Create_Call, MockStaffRepository_Delete_Call, MockStaffRepository_Expecter, MockStaffRepository_FindByID_Call, MockStaffRepository_FindByUser_Call, MockStaffRepository_List_Call, MockStaffRepository_SetGroups_Call, MockStaffRepository_UpdateStatus_Call

### Community 118 - ".Return"
Cohesion: 0.08
Nodes (8): MockUserRepository_Create_Call, MockUserRepository_Expecter, MockUserRepository_FindByEmail_Call, MockUserRepository_FindByGoogleSubject_Call, MockUserRepository_FindByID_Call, MockUserRepository_FindByPhone_Call, MockUserRepository_GrantPlatformAdmins_Call, MockUserRepository_Update_Call

### Community 119 - "NotificationUseCase"
Cohesion: 0.09
Nodes (14): Server, logToProto(), NewServer(), pageOf(), formatPeriodEnd(), FormatPeriodEndForTest(), Deps, NotificationUseCase (+6 more)

### Community 120 - "portal_test.go"
Cohesion: 0.11
Nodes (19): CustomerRef, assertAppError(), garages, users, vehicles, newWorld(), newWorldWithoutLinks(), portal() (+11 more)

### Community 121 - "quote/handler/mapping.go"
Cohesion: 0.37
Nodes (8): TestParseQuantity(), TestQuote_Recalculate(), ParseQuantity(), clock, EventPublisher, Sequencer, TransactionLimiter, VehicleReader

### Community 122 - "newGarageUseCase"
Cohesion: 0.17
Nodes (28): MockNotificationReceipts, NewMockNotificationReceipts(), NewMockNotificationRepository(), staffLan(), TestACustomerFailureDoesNotBlockTheStaff(), TestAGarageSeesAShortReasonNotTheProvidersAnswer(), TestAnEmailThatWentOutIsNotUndoneByALogFailure(), TestEventsAreNotSentTwice() (+20 more)

### Community 123 - "StockTransaction"
Cohesion: 0.09
Nodes (17): TestRefType(), AddQuantities(), RefType, StockTransaction, TransactionType, refTypeFromProto(), refTypeToProto(), transactionTypeFromProto() (+9 more)

### Community 124 - "toAppError"
Cohesion: 0.10
Nodes (12): TestDefaultRoles(), ValidRoleName(), StaffStatus, staffStatusFromProto(), staffStatusToProto(), toAppError(), GroupFields, applyRoleFields() (+4 more)

### Community 125 - "repairOrderToProto"
Cohesion: 0.09
Nodes (8): badMask(), maskPaths(), run(), pageRequest(), repairOrderToProto(), Server, lineEdit(), PageRequest

### Community 126 - "Supplier"
Cohesion: 0.11
Nodes (22): TestCheckLength(), CheckLength(), BankDetails, Contact, Supplier, newSupplier(), translate(), actorRef() (+14 more)

### Community 127 - "StockCount"
Cohesion: 0.08
Nodes (14): Tally, TestStockCountStatusAndCountedQuantity(), StockCount, StockCountStatus, ParseCountedQuantity(), countStatusFromProto(), countStatusToProto(), newCount() (+6 more)

### Community 128 - "UseCase"
Cohesion: 0.12
Nodes (14): Rule, UserObjectKey(), Server, NewServer(), toAppError(), actorRef(), DeleteRequest, Result (+6 more)

### Community 129 - "dataimport/usecase/ports.go"
Cohesion: 0.11
Nodes (16): AuditLogger, CountInput, Document, NewPart, ReceiptInput, MockImportInventory, NewMockImportInventory(), MockImportInventory_CreateGoodsReceipt_Call (+8 more)

### Community 131 - "Event"
Cohesion: 0.10
Nodes (12): decodeRepairOrder(), partLinePayload, repairOrderPayload, creators(), isGone(), Event, shortageEvent(), errorsIsNotFound() (+4 more)

### Community 132 - "newEnv"
Cohesion: 0.14
Nodes (30): TestPublicToken(), HashPublicToken(), NewPublicToken(), TestConvertQuote(), TestConvertQuote_NotFound(), TestRecordQuoteDecision(), TestRecordQuoteDecision_NotFound(), TestSendQuote() (+22 more)

### Community 133 - ".Return"
Cohesion: 0.09
Nodes (7): MockFileRepository_Create_Call, MockFileRepository_Expecter, MockFileRepository_FindByID_Call, MockFileRepository_FindByKey_Call, MockFileRepository_KeyByID_Call, MockFileRepository_SetActive_Call, MockFileRepository_SoftDelete_Call

### Community 134 - ".Return"
Cohesion: 0.09
Nodes (7): MockInvitationRepository_Create_Call, MockInvitationRepository_Expecter, MockInvitationRepository_ExpireStale_Call, MockInvitationRepository_FindByID_Call, MockInvitationRepository_ListByGarage_Call, MockInvitationRepository_ListByUser_Call, MockInvitationRepository_Update_Call

### Community 135 - "flow.js"
Cohesion: 0.11
Nodes (30): API_URL, awaitEvent(), browse(), BROWSE_VUS, businessTransactions, call(), dashboard(), ensureSession() (+22 more)

### Community 136 - "Type"
Cohesion: 0.11
Nodes (25): importWorkbook(), Type, ParseType(), TestParseType(), as(), env, headerOnly(), newEnv() (+17 more)

### Community 137 - "Period"
Cohesion: 0.10
Nodes (21): revenuePeriodLabel(), revenuePeriod(), date(), sameInstant(), TestGetRevenueReport(), TestPeriodFromProto(), TestReportToProto(), TestRequestFromProto() (+13 more)

### Community 138 - "GarageHeader"
Cohesion: 0.08
Nodes (20): All(), billTo(), garage(), lines(), BillTo, DocumentLine, GarageHeader, GoodsReceipt (+12 more)

### Community 139 - "CustomerInfo"
Cohesion: 0.10
Nodes (10): MockNotificationCustomerReader, NewMockNotificationCustomerReader(), CustomerInfo, MockNotificationCustomerReader_ByVehicle_Call, MockNotificationCustomerReader_Expecter, MockNotificationCustomerReader_Get_Call, MockNotificationCustomerReader_OptOut_Call, CustomerReader (+2 more)

### Community 140 - "UserInfo"
Cohesion: 0.09
Nodes (12): MockNotificationStaffReader, NewMockNotificationStaffReader(), MockNotificationUserReader, NewMockNotificationUserReader(), deps, StaffReader, UserInfo, MockNotificationStaffReader_Expecter (+4 more)

### Community 141 - "Issuer"
Cohesion: 0.09
Nodes (14): PaymentRow, findVoucher(), SettlementInfo, SettlementReader, MockDocumentSettlementReader, NewMockDocumentSettlementReader(), MockReceivableIssuerReader, NewMockReceivableIssuerReader() (+6 more)

### Community 142 - "newUseCase"
Cohesion: 0.17
Nodes (26): expectCreate(), TestSettlementUseCase_Create_BillingParty(), TestSettlementUseCase_RecordPayment_PayerDefaultsToTheBillingParty(), TestSettlementUseCase_Update(), assertAppError(), closedRepairOrder(), defaultGarage(), expectAudit() (+18 more)

### Community 143 - "Idempotency"
Cohesion: 0.12
Nodes (16): Probe, scheduleIdempotencyPurge(), complete(), Idempotency(), idempotencyKey(), printableASCII(), replay(), requestHash() (+8 more)

### Community 144 - "Line"
Cohesion: 0.12
Nodes (13): Line, scanLine(), scanRepairOrder(), applyProgress(), checkEditable(), consumesStock(), equalRef(), LineEdit (+5 more)

### Community 145 - ".UpdateSupplier"
Cohesion: 0.11
Nodes (8): badMask(), run(), stringField(), bankFromProto(), contactFromProto(), supplierToProto(), Server, wholeBlockPaths()

### Community 146 - "Run"
Cohesion: 0.11
Nodes (8): PostgresRepository, Run, MockImportRunRepository, NewMockImportRunRepository(), MockImportRunRepository_Create_Call, MockImportRunRepository_Expecter, MockImportRunRepository_Find_Call, MockImportRunRepository_Lock_Call

### Community 147 - "newUseCase"
Cohesion: 0.14
Nodes (27): NewDocumentUseCase(), assertAppError(), defaultGarage(), logoPNG(), newUseCase(), TestDocumentUseCase_InventoryDocuments(), TestDocumentUseCase_Letterhead(), TestDocumentUseCase_PayablesNotWired() (+19 more)

### Community 148 - "MockLogoFiles"
Cohesion: 0.10
Nodes (7): NewMockLogoFiles(), MockLogoFiles, MockLogoFiles_Discard_Call, MockLogoFiles_Expecter, MockLogoFiles_PublicURL_Call, MockLogoFiles_Retire_Call, MockLogoFiles_StoreLogo_Call

### Community 149 - ".Return"
Cohesion: 0.11
Nodes (6): MockDataRequestRepository_ClearDetails_Call, MockDataRequestRepository_Create_Call, MockDataRequestRepository_Expecter, MockDataRequestRepository_Find_Call, MockDataRequestRepository_FindForUpdate_Call, MockDataRequestRepository_Save_Call

### Community 150 - "billing/usecase/repository.go"
Cohesion: 0.20
Nodes (11): TestSettlement_DeriveStatus(), TestSettlement_RemainingDue(), TestPayment_Voided(), TestPaymentMethod_IsValid(), TestSettlementStatus_IsValid(), TestPolicies(), clock, EventPublisher (+3 more)

### Community 151 - "Consumer"
Cohesion: 0.12
Nodes (7): DLQTopic(), TestTopicFor(), TopicFor(), Handler, Consumer, Producer, Dispatcher

### Community 152 - "PayableUseCase"
Cohesion: 0.11
Nodes (9): Aging, agingToProto(), PayableDeps, PayableUseCase, NewPayableUseCase(), AgingResult, IssuerReader, PayableRepository (+1 more)

### Community 153 - ".Return"
Cohesion: 0.11
Nodes (6): MockFileStorage_Delete_Call, MockFileStorage_Expecter, MockFileStorage_PublicURL_Call, MockFileStorage_Put_Call, MockFileStorage_PutPrivate_Call, MockFileStorage_SignedURL_Call

### Community 154 - ".Return"
Cohesion: 0.11
Nodes (6): MockSupplierRepository_Create_Call, MockSupplierRepository_Expecter, MockSupplierRepository_FindByID_Call, MockSupplierRepository_SetActive_Call, MockSupplierRepository_SoftDelete_Call, MockSupplierRepository_Update_Call

### Community 155 - "IdentityUseCase"
Cohesion: 0.10
Nodes (13): env, IdentityUseCase, NewIdentityUseCase(), Claims, Identity, TokenIssuer, NewTokenIssuer(), newTestIssuer() (+5 more)

### Community 156 - "StaffUseCase"
Cohesion: 0.13
Nodes (14): Type, TestType_IsValid(), SetStaffClock(), act(), exportOf(), CreateRequest, ListRequest, StaffUseCase (+6 more)

### Community 157 - "support/repository/postgres_integration_test.go"
Cohesion: 0.17
Nodes (20): at(), join(), newReportFixture(), TestCompletedWork_CountsLinesOrdersAndTurnaround(), TestLineTimes_FollowTheStatusOfTheLine(), TestProfitLines_ClosedInThePeriodInTheGarageZone(), NewGrant(), TestGrantLifecycle() (+12 more)

### Community 158 - "Mailpit"
Cohesion: 0.13
Nodes (5): MailjetSender, kit, Mailpit, Message, isPasswordResetMail()

### Community 159 - "Tx"
Cohesion: 0.10
Nodes (5): expectation, kind, Rows, Tx, assign()

### Community 160 - "PurchaseOrder"
Cohesion: 0.11
Nodes (9): PurchaseOrder, PurchaseOrderStatus, TestPurchaseOrderStatus_IsValid(), orderStatusFromProto(), orderStatusToProto(), scanOrder(), newOrder(), PurchaseOrderFilter (+1 more)

### Community 161 - "newEnv"
Cohesion: 0.11
Nodes (16): MockNotificationSuppressions, NewMockNotificationSuppressions(), MockNotificationTransactor, NewMockNotificationTransactor(), call(), env, newEnv(), TestGetNotificationSettings() (+8 more)

### Community 162 - "profit.go"
Cohesion: 0.16
Nodes (17): buildProfit(), costKnown(), distinct(), ProfitGroupBy, orderIDs(), sharesOf(), split(), calendarDate() (+9 more)

### Community 163 - "ReconciliationData"
Cohesion: 0.11
Nodes (10): TestReconciliationExcel(), ReceivableRow, ReconciliationData, ReconciliationExcel(), MockDocumentReceivableReader_Expecter, MockDocumentReceivableReader_Receivables_Call, MockDocumentReceivableReader_Reconciliation_Call, MethodAmount (+2 more)

### Community 164 - "newEnv"
Cohesion: 0.13
Nodes (17): env, logo(), newEnv(), reason(), signedIn(), TestDeleteFile(), TestGetFileUrl(), TestGetFileUrlSigned() (+9 more)

### Community 165 - "Deps"
Cohesion: 0.09
Nodes (10): newClock(), oilChange(), Deps, NewQuoteUseCase(), Job, MockQuoteJobCatalogue_Job_Call, JobCatalogue, JobPart (+2 more)

### Community 166 - "Status"
Cohesion: 0.10
Nodes (10): TestStatus(), Status, public(), decisionsFromProto(), publicQuoteToProto(), statusFromProto(), statusToProto(), zeroTime() (+2 more)

### Community 167 - "checkedFile"
Cohesion: 0.18
Nodes (14): Preview, Result, RowError, SampleRow, clamp(), countMap(), importTypeToProto(), previewToProto() (+6 more)

### Community 168 - "billing/handler/branches_test.go"
Cohesion: 0.24
Nodes (22): domainMethod(), TestCreateSettlementAlreadySettled(), TestGetSettlementPartiallyPaid(), TestGetSettlementUnknownStatus(), TestListSettlementsFailure(), TestListSettlementsFilters(), TestListSettlementsPaginationClamps(), TestRecordSettlementPaymentInvalidMethod() (+14 more)

### Community 169 - "MockInventoryDirectory"
Cohesion: 0.12
Nodes (7): SupplierContact, MockInventoryDirectory, NewMockInventoryDirectory(), MockInventoryDirectory_Expecter, MockInventoryDirectory_SupplierContacts_Call, MockInventoryDirectory_SupplierNames_Call, MockInventoryDirectory_UserName_Call

### Community 170 - "UseCase"
Cohesion: 0.17
Nodes (17): NewMockSupplierRepository(), MockSupplierUsageChecker, NewMockSupplierUsageChecker(), Server, NewServer(), env, newEnv(), DeleteRequest (+9 more)

### Community 171 - "env"
Cohesion: 0.10
Nodes (9): env, MockBillingCollectorReader, NewMockBillingCollectorReader(), MockBillingSequencer, NewMockBillingSequencer(), MockBillingCollectorReader_Expecter, MockBillingCollectorReader_FullName_Call, MockBillingSequencer_Expecter (+1 more)

### Community 172 - "File"
Cohesion: 0.13
Nodes (7): File, PostgresRepository, nullable(), scanFile(), UsageChecker, MockFileRepository, NewMockFileRepository()

### Community 173 - "MockEntitlementCache"
Cohesion: 0.13
Nodes (6): NewMockEntitlementCache(), MockEntitlementCache, MockEntitlementCache_Delete_Call, MockEntitlementCache_Expecter, MockEntitlementCache_Get_Call, MockEntitlementCache_Set_Call

### Community 174 - "DocumentUseCase"
Cohesion: 0.13
Nodes (12): documentServer, Server, NewServer(), TestReceivablesExcel(), TestSupplierPayablesExcel(), ReceivablesExcel(), SupplierPayablesExcel(), write() (+4 more)

### Community 175 - "CustomerUseCase"
Cohesion: 0.15
Nodes (6): actorRef(), CustomerUseCase, CreateCustomerRequest, CustomerRepository, DeleteCustomerRequest, UpdateCustomerRequest

### Community 176 - "ExpiryJob"
Cohesion: 0.17
Nodes (3): ExpiryJob, NewExpiryJob(), SubscriptionRepository

### Community 177 - "MigrationsTestSuite"
Cohesion: 0.14
Nodes (5): ForceMigrationVersion(), GetMigrationVersion(), RollbackAllMigrations(), RollbackLastMigration(), MigrationsTestSuite

### Community 178 - "UseCase"
Cohesion: 0.16
Nodes (10): Server, NewServer(), actorRef(), Dependencies, Request, UseCase, NewUseCase(), dataImportModule (+2 more)

### Community 179 - "NewTechnicianUseCase"
Cohesion: 0.14
Nodes (15): Server, NewServer(), signedIn(), TestListMyWork(), TestListMyWork_Error(), assignedTo(), Deps, Memberships (+7 more)

### Community 180 - "PaymentOutInfo"
Cohesion: 0.14
Nodes (8): PaymentOutReceiptRow, PaymentOutVoucher, PayableRow, PaymentOutInfo, MockDocumentPayableReader_Expecter, MockDocumentPayableReader_PaymentOutVoucher_Call, MockDocumentPayableReader_SupplierPayables_Call, documentPayables

### Community 181 - "MockSupplierRepository"
Cohesion: 0.11
Nodes (4): MockSupplierRepository, ListFilter, Repository, MockSupplierRepository_List_Call

### Community 182 - "GoodsReceiptStatus"
Cohesion: 0.13
Nodes (8): TestGoodsReceiptStatus_IsValid(), GoodsReceiptStatus, receiptStatusFromProto(), receiptStatusToProto(), PostgresRepository, scanReceipt(), GoodsReceiptFilter, ListGoodsReceiptsRequest

### Community 183 - ".verifier"
Cohesion: 0.23
Nodes (14): google, Option, NewVerifier(), newGoogle(), TestVerify_AcceptsAValidToken(), TestVerify_AcceptsTheOtherIssuerSpellingAndAStringEmailVerified(), TestVerify_CachesTheKeysAndRefetchesForANewKeyID(), TestVerify_FailsWhenTheKeysCannotBeFetched() (+6 more)

### Community 184 - "Line"
Cohesion: 0.15
Nodes (12): TestItemType(), ItemType, Line, LineDecision, decisionToProto(), derefString(), itemTypeFromProto(), itemTypeToProto() (+4 more)

### Community 185 - ".buildLine"
Cohesion: 0.13
Nodes (16): TestItemType_IsValid(), CheckLength(), ItemType, LineTotal(), ParseQuantity(), TestCheckLength(), TestLineTotal(), TestParseQuantity() (+8 more)

### Community 186 - "newFixture"
Cohesion: 0.20
Nodes (16): TestPurchaseOrder_RecountAndTotals(), TestShortages(), balance(), fixture, newFixture(), TestInventoryRepository_AverageCost(), TestInventoryRepository_GoodsReceipt(), TestInventoryRepository_PartSuppliers() (+8 more)

### Community 187 - "db_tracer_test.go"
Cohesion: 0.16
Nodes (4): Init(), TestInit(), queryTracerSpanKey, Shutdown

### Community 188 - ".create"
Cohesion: 0.16
Nodes (9): Server, NewServer(), sourceIP(), Mailer, Repository, UseCase, orDash(), truncate() (+1 more)

### Community 189 - "newWorld"
Cohesion: 0.24
Nodes (14): world, TestLockPayablesSerializesAllocations(), TestPayablesAddCancelListAndAging(), TestVoucherLifecycleAndConstraints(), world, newWorld(), TestAdvisoryLockIsExclusive(), TestLedgerPeriodQueries() (+6 more)

### Community 190 - "Deps"
Cohesion: 0.13
Nodes (5): Limit, Deps, MockLimitChecker_CheckLimit_Call, Limits, LimitChecker

### Community 191 - "importRequest"
Cohesion: 0.17
Nodes (4): importRequest(), importType(), ToGRPCError(), importPath()

### Community 192 - "Sheet"
Cohesion: 0.15
Nodes (9): stubReader, deps, Sheet, MockImportAuditLogger, NewMockImportAuditLogger(), MockImportReader_Read_Call, dataImportReader, fakeReader (+1 more)

### Community 193 - "contactrequest/usecase/usecase_test.go"
Cohesion: 0.30
Nodes (13): New(), input(), newUC(), TestCreateCountsAnIPv6SourceByItsNetwork(), TestCreateHashesTheSameSourceTheSameWay(), TestCreateKeepsTheRequestWhenTheMailFails(), TestCreateMailsEvenWhenTheCallerGoesAway(), TestCreateRefusesWithoutStoring() (+5 more)

### Community 194 - ".CreateStockIssue"
Cohesion: 0.17
Nodes (8): TestParseQuantity(), ParseQuantity(), StockIssueLine, isNotFound(), linePartIDs(), issuedLines(), CreateStockIssueRequest, IssueLineInput

### Community 195 - "authorizeGarage"
Cohesion: 0.18
Nodes (11): allowedWhenReadOnly(), isFileReadMethod(), isReadMethod(), LicenseChecker, LicenseMode, authorizeSupport(), SupportSessions, authorizeGarage() (+3 more)

### Community 196 - "MockCatalogueFiles_Expecter"
Cohesion: 0.17
Nodes (4): MockCatalogueFiles_Expecter, MockCatalogueFiles_PartImage_Call, MockCatalogueFiles_Release_Call, MockCatalogueFiles_URLs_Call

### Community 197 - "MockCatalogueUsageChecker_Expecter"
Cohesion: 0.17
Nodes (4): MockCatalogueUsageChecker_Expecter, MockCatalogueUsageChecker_JobInUse_Call, MockCatalogueUsageChecker_PartInUse_Call, MockCatalogueUsageChecker_UnitInUse_Call

### Community 198 - "MockCustomerUsageChecker_CustomerInUse_Call"
Cohesion: 0.17
Nodes (4): MockCustomerUsageChecker_CustomerInUse_Call, MockCustomerUsageChecker_Expecter, MockCustomerUsageChecker_VehicleInUse_Call, MockCustomerUsageChecker_VehiclesInUse_Call

### Community 199 - ".GetProfitReport"
Cohesion: 0.15
Nodes (5): datesOf(), Server, groupByFromProto(), requireCost(), toInt32()

### Community 200 - "buildMIME"
Cohesion: 0.13
Nodes (10): safeFileName(), SMTPSender, Attachment, validate(), address(), buildMIME(), SMTPSender, sortedKeys() (+2 more)

### Community 201 - "VietGara Backend"
Cohesion: 0.12
Nodes (15): events, CI secrets and variables (staging workflows), Configuration, Container image and staging, Database roles, Development, Events, File storage (+7 more)

### Community 202 - "contactrequest/handler/http_test.go"
Cohesion: 0.24
Nodes (12): repo, Request, Status, post(), setup(), TestAcceptsARequestWithTheKey(), TestAnswers429WhenTheSourceIsOverItsLimit(), TestCountsTheVisitorNotTheProxy() (+4 more)

### Community 203 - "PurchaseLine"
Cohesion: 0.17
Nodes (7): PurchaseLine, PostgresRepository, checkRange(), PurchaseFilter, MockInventoryRepository_PurchasesReport_Call, PurchasesReport, PurchasesReportRequest

### Community 204 - "MockTransactionCounter"
Cohesion: 0.17
Nodes (5): NewMockTransactionCounter(), MockTransactionCounter, MockTransactionCounter_Expecter, MockTransactionCounter_Get_Call, MockTransactionCounter_Increment_Call

### Community 205 - "MockUsageReader"
Cohesion: 0.16
Nodes (5): NewMockUsageReader(), MockUsageReader, MockUsageReader_Expecter, MockUsageReader_LockOwnerUsage_Call, MockUsageReader_OwnerUsage_Call

### Community 206 - "toAppError"
Cohesion: 0.18
Nodes (6): actorRef(), isAny(), preconditionReason(), toAppError(), CreateFromQuoteRequest, CreateRepairOrderRequest

### Community 207 - "RepairOrderStatus"
Cohesion: 0.14
Nodes (8): TestRepairOrderStatus_IsValid(), RepairOrderStatus, statusFromProto(), statusToProto(), line(), order(), TestListMyWork_KeepsOpenOrdersAndOnlyMyLines(), ListRepairOrdersRequest

### Community 208 - "Verifier"
Cohesion: 0.19
Nodes (5): fakeVerifier, GoogleIDTokenVerifier, Claims, Verifier, rsaKey()

### Community 209 - "assertAppError"
Cohesion: 0.22
Nodes (14): TestConfirmations_WorkOnceAndExpire(), assertAppError(), confirmation(), newPublic(), TestPublicUseCase_Confirm(), TestPublicUseCase_ConfirmFailures(), TestPublicUseCase_ConfirmGivesTheLinkBackEvenWhenThatFails(), TestPublicUseCase_ConfirmNamesAGarageOnce() (+6 more)

### Community 210 - "toAppError"
Cohesion: 0.30
Nodes (3): billTo(), GarageReader, toAppError()

### Community 211 - "Reminder"
Cohesion: 0.20
Nodes (7): MockReceivableReminderSender, NewMockReceivableReminderSender(), Delivery, Reminder, MockReceivableReminderSender_Expecter, MockReceivableReminderSender_SendPaymentReminder_Call, receivableReminders

### Community 212 - "StockMovementRow"
Cohesion: 0.18
Nodes (7): movementReference(), StockMovementRow, StockMovementsExcel(), TestStockMovementsExcel(), MockDocumentStockMovementReader_List_Call, documentStockMovements, StockMovementReader

### Community 213 - ".sendOTPFor"
Cohesion: 0.22
Nodes (8): OTPPurpose, HashOTP(), NewOTP(), NewOTPCode(), OTPKey(), TestOTP(), Challenge, resendTooSoonError

### Community 214 - "Kind"
Cohesion: 0.18
Nodes (7): TestKind(), TestObjectKey(), Kind, ObjectKey(), fileToProto(), kindFromProto(), kindToProto()

### Community 215 - "GarageInfo"
Cohesion: 0.21
Nodes (6): GarageInfo, GarageReader, MockBillingGarageReader, NewMockBillingGarageReader(), MockBillingGarageReader_Expecter, MockBillingGarageReader_Get_Call

### Community 216 - "RepairOrderInfo"
Cohesion: 0.21
Nodes (6): RepairOrderInfo, RepairOrderReader, MockBillingRepairOrderReader, NewMockBillingRepairOrderReader(), MockBillingRepairOrderReader_Expecter, MockBillingRepairOrderReader_Get_Call

### Community 217 - "newEnv"
Cohesion: 0.32
Nodes (13): as(), assertAppError(), newEnv(), storedLogo(), TestUseCase_Delete(), TestUseCase_FutureKinds(), TestUseCase_PublicURL(), TestUseCase_RetireByKey() (+5 more)

### Community 218 - "OrderMail"
Cohesion: 0.19
Nodes (5): OrderMail, MockOrderMailer, MockOrderMailer_Expecter, MockOrderMailer_SendPurchaseOrder_Call, OrderMailer

### Community 219 - "MockNotificationGarageReader"
Cohesion: 0.21
Nodes (6): MockNotificationGarageReader, NewMockNotificationGarageReader(), GarageInfo, GarageReader, MockNotificationGarageReader_Expecter, MockNotificationGarageReader_Get_Call

### Community 220 - "TestConsumer"
Cohesion: 0.29
Nodes (13): NewConsumer(), WithClientOpts(), WithRetry(), event(), newCluster(), readTopic(), TestConsumer(), TestConsumer_CancelledWhileRetrying() (+5 more)

### Community 221 - "Load test (Release 9.0, NFR-PF-06)"
Cohesion: 0.17
Nodes (8): Event latency (NFR-PF-05), Limits of this test, Load test (Release 9.0, NFR-PF-06), Results, Run it, The flow, The load, The population

### Community 222 - "HistoryEntry"
Cohesion: 0.24
Nodes (7): HistoryEntryToProto(), historyEntryToProto(), historyEntryTypeToProto(), HistoryEntry, HistoryEntryType, isDraftQuote(), MockHistoryReader_History_Call

### Community 223 - "Deps"
Cohesion: 0.17
Nodes (3): Deps, NewSettlementUseCase(), CollectorReader

### Community 224 - "MockNotificationReceipts_PurchaseOrder_Call"
Cohesion: 0.23
Nodes (3): MockNotificationReceipts_Expecter, MockNotificationReceipts_PurchaseOrder_Call, MockNotificationReceipts_SettlementReceipt_Call

### Community 225 - "MockNotificationSuppressions_Suppress_Call"
Cohesion: 0.23
Nodes (3): MockNotificationSuppressions_Expecter, MockNotificationSuppressions_Suppress_Call, MockNotificationSuppressions_Suppressed_Call

### Community 226 - "MockRepairOrderStockIssues_Posted_Call"
Cohesion: 0.23
Nodes (3): MockRepairOrderStockIssues_Expecter, MockRepairOrderStockIssues_Posted_Call, MockRepairOrderStockIssues_Short_Call

### Community 227 - "MockReportingNameReader_JobNames_Call"
Cohesion: 0.23
Nodes (3): MockReportingNameReader_Expecter, MockReportingNameReader_JobNames_Call, MockReportingNameReader_UserNames_Call

### Community 228 - "numbering.go"
Cohesion: 0.30
Nodes (6): Period(), Render(), renderDateToken(), renderSeq(), TestPeriod(), TestRender()

### Community 229 - "Blackbox tests"
Cohesion: 0.17
Nodes (12): Blackbox tests, Fetching the shared dataset, How it differs from the other tests, Layout, Purging test users, Roadmap: Phase 4, Quality Foundation, Seeding the local baseline, `staging` (+4 more)

### Community 230 - "UserSummary"
Cohesion: 0.29
Nodes (5): toStaffSummary(), Users, UserDirectory, UserSummary, platformUserDirectory

### Community 231 - ".EnrollMFA"
Cohesion: 0.25
Nodes (10): EncodeTOTPSecret(), MatchTOTP(), NewTOTPSecret(), OTPAuthURI(), TestMatchTOTP(), TestOTPAuthURI(), TestTOTPCodeRFC6238Vectors(), TOTPCode() (+2 more)

### Community 232 - "GarageSettings"
Cohesion: 0.24
Nodes (4): Garages, GarageSettings, MockInventoryGarages_Expecter, MockInventoryGarages_Get_Call

### Community 233 - "results/README.md"
Cohesion: 0.18
Nodes (6): Load test summary, Load test summary, Load test summary, Load test summary, Load test results, Reading these numbers

### Community 234 - "Database Migrations & Row-Level Security (RLS)"
Cohesion: 0.20
Nodes (9): EnsureAppLogin(), validRoleName(), Database Migrations & Row-Level Security (RLS), Migrations, Roles and connections, Row-Level Security, Running migrations, Schema (Release 1.1) (+1 more)

### Community 235 - "SettlementStatus"
Cohesion: 0.22
Nodes (4): SettlementStatus, statusFromProto(), statusToProto(), ListSettlementsRequest

### Community 236 - "sampled"
Cohesion: 0.38
Nodes (3): newSampled(), poolCollector, sampled

### Community 237 - "DocumentNumbers"
Cohesion: 0.24
Nodes (3): DocumentNumbers, MockDataRequestDocuments_Expecter, MockDataRequestDocuments_Numbers_Call

### Community 238 - "MockBillingEventPublisher"
Cohesion: 0.22
Nodes (4): MockBillingEventPublisher, NewMockBillingEventPublisher(), MockBillingEventPublisher_Expecter, MockBillingEventPublisher_Publish_Call

### Community 239 - "partquantity.go"
Cohesion: 0.33
Nodes (5): MockPartQuantityResolver_ResolveQuantity_Call, Line, ForLine(), Resolved, Resolver

### Community 240 - "PaymentMethod"
Cohesion: 0.25
Nodes (7): TestVoucherMethod(), voucherMethod(), PaymentMethod, methodFromProto(), methodToProto(), methodFromProto(), RecordPaymentRequest

### Community 242 - "GoogleOAuthConfig"
Cohesion: 0.36
Nodes (6): googleTokenResponse, googleUserInfo, GoogleOAuthConfig, exchangeGoogleCode(), fetchGoogleToken(), fetchGoogleUserInfo()

### Community 243 - "newEnv"
Cohesion: 0.44
Nodes (9): existingCustomer(), TestCustomerRPCs(), newEnv(), reason(), signedIn(), existingVehicle(), TestCorrectVehicleAndChangeOwner(), TestLockedVehicleIsMappedToItsFields() (+1 more)

### Community 244 - "MemoryTransactionCounter"
Cohesion: 0.33
Nodes (4): counterKey(), MemoryTransactionCounter, NewRedisTransactionCounter(), RedisTransactionCounter

### Community 245 - "internalError"
Cohesion: 0.25
Nodes (4): internalError, hasReason(), reasonFor(), rejectsValue()

### Community 247 - "QuoteReady"
Cohesion: 0.32
Nodes (3): QuoteReady, MockQuoteNotifier_QuoteReady_Call, quoteNotifier

### Community 249 - "Membership"
Cohesion: 0.33
Nodes (4): memberships, Membership, memberships, technicianMemberships

### Community 250 - "GarageInfo"
Cohesion: 0.38
Nodes (3): GarageInfo, GarageReader, MockGarageReader_Get_Call

### Community 251 - "line"
Cohesion: 0.43
Nodes (7): hidden(), line(), orderOf(), TestDeriveStatus_FinalStatusesStay(), TestDeriveStatus_HiddenConsumablesNeverHoldTheOrderBack(), TestDeriveStatus_WaitingOnParts(), TestReadyToClose_NeedsABillableLine()

### Community 271 - ".List"
Cohesion: 0.40
Nodes (3): containsPattern(), phonePattern(), scanSupplier()

### Community 274 - "E-mail"
Cohesion: 0.40
Nodes (5): E-mail, E-mails of a garage (Release 10.1), Receivables (Release 13.0), Setting up Mailjet, Test users

### Community 275 - ".Get"
Cohesion: 0.50
Nodes (3): TestDocumentQuantity(), documentQuantity(), documentQuotes

## Knowledge Gaps
- **13 isolated node(s):** `github.com/viettechno/vietgara-backend`, `Server`, `Server`, `Server`, `env` (+8 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 1095 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **42 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `LocalStore` connect `LocalStore` to `time.Time`, `wiring.go`, `Pool`?**
  _High betweenness centrality (0.010) - this node is a cross-community bridge._
- **What connects `github.com/viettechno/vietgara-backend`, `Server`, `Server` to the rest of the system?**
  _13 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `context.Context` be split into smaller, more focused modules?**
  _Cohesion score 0.008240085654903114 - nodes in this community are weakly interconnected._
- **Why does `PageRequest` connect `PageRequest` to `wiring.go`?**
  _High betweenness centrality (0.010) - this node is a cross-community bridge._
- **Should `ToGRPCError` be split into smaller, more focused modules?**
  _Cohesion score 0.008857174154578032 - nodes in this community are weakly interconnected._
- **Why does `Server` connect `QuoteUseCase` to `server/main.go`, `Pool`, `github.com/stretchr/testify/mock.Mock`?**
  _High betweenness centrality (0.010) - this node is a cross-community bridge._
- **Should `go_pkg_context` be split into smaller, more focused modules?**
  _Cohesion score 0.14772907105554675 - nodes in this community are weakly interconnected._