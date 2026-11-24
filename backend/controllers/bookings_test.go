package controllers

import (
	"encoding/json"
	"strings"
	"testing"

	"travo/backend/models"
)

func TestDriverPhoneOnlyOnPaidConfirmedPassengerBooking(t *testing.T) {
	phone := "+237 650 123 456"
	booking := models.Booking{Trip: models.Trip{Driver: models.User{Phone: &phone}}}
	for _, test := range []struct {
		status, payment string
		wantPhone       bool
	}{
		{"pending_payment", "not_started", false},
		{"pending_payment", "SUCCESSFUL", false},
		{"confirmed", "PENDING", false},
		{"cancelled", "SUCCESSFUL", false},
		{"confirmed", "SUCCESSFUL", true},
	} {
		booking.Status, booking.PaymentStatus = test.status, test.payment
		data, err := json.Marshal(passengerBookingView(booking))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), phone) != test.wantPhone {
			t.Errorf("status=%s payment=%s: %s", test.status, test.payment, data)
		}
		driverData, err := json.Marshal(toBookingView(booking))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(driverData), phone) {
			t.Errorf("phone exposed in non-passenger view: %s", driverData)
		}
	}
}

func TestCalculateBookingAmounts(t *testing.T) {
	tests := []struct {
		name            string
		fare            int64
		wantCommission  int64
		wantCharged     int64
		wantProviderFee int64
	}{
		{name: "1000 XAF fare", fare: 1000, wantCommission: 50, wantCharged: 1031, wantProviderFee: 31},
		{name: "small fare rounds commission up", fare: 100, wantCommission: 5, wantCharged: 104, wantProviderFee: 4},
		{name: "multi-seat fare", fare: 3000, wantCommission: 150, wantCharged: 3093, wantProviderFee: 93},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			commission, charged, providerFee := calculateBookingAmounts(test.fare)
			if commission != test.wantCommission || charged != test.wantCharged || providerFee != test.wantProviderFee {
				t.Fatalf("got commission=%d charged=%d providerFee=%d; want %d, %d, %d",
					commission, charged, providerFee,
					test.wantCommission, test.wantCharged, test.wantProviderFee)
			}
		})
	}
}
