package handler

import (
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
)

var typeToProto = map[domain.Type]notificationv1.NotificationType{
	domain.TypeQuoteReady:        notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_READY,
	domain.TypeQuoteDecided:      notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_DECIDED,
	domain.TypeVehicleReady:      notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY,
	domain.TypePaymentReceived:   notificationv1.NotificationType_NOTIFICATION_TYPE_PAYMENT_RECEIVED,
	domain.TypeSettlementReceipt: notificationv1.NotificationType_NOTIFICATION_TYPE_SETTLEMENT_RECEIPT,
	domain.TypeSettlementPaid:    notificationv1.NotificationType_NOTIFICATION_TYPE_SETTLEMENT_PAID,
	domain.TypeLowStock:          notificationv1.NotificationType_NOTIFICATION_TYPE_LOW_STOCK,
}

// typeFromProto maps an unspecified or unknown type to the empty type.
var typeFromProto = invert(typeToProto)

var statusToProto = map[domain.Status]notificationv1.NotificationStatus{
	domain.StatusSent:    notificationv1.NotificationStatus_NOTIFICATION_STATUS_SENT,
	domain.StatusFailed:  notificationv1.NotificationStatus_NOTIFICATION_STATUS_FAILED,
	domain.StatusSkipped: notificationv1.NotificationStatus_NOTIFICATION_STATUS_SKIPPED,
}

var statusFromProto = invert(statusToProto)

var reasonToProto = map[domain.SkipReason]notificationv1.NotificationSkipReason{
	domain.SkipDisabled:   notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_DISABLED,
	domain.SkipOptedOut:   notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_OPTED_OUT,
	domain.SkipSuppressed: notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_SUPPRESSED,
	domain.SkipNoEmail:    notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_NO_EMAIL,
}

func invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}

	return out
}
