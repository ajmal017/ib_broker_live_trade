package ibkr

import (
	"testing"
	"time"
)

func TestAlive(t *testing.T) {
	err := CheckAuthenticated()
	if err != nil {
		panic(err)
	}
	KeepAlive()
}

func TestInitializeConIDs(t *testing.T) {
	go KeepAlive()
	initializeConIDs()
}

func TestPlaceOrder(t *testing.T) {
	InitializeServer()

	// This is REAL BUYING!!
	orderID, order, err := PlaceOrder("SCHW", SecType.STK, OrderType.MKT, OrderSide.SELL, Tif.DAY, -1, 2)
	if err != nil {
		panic(err)
	}
	for range time.Tick(time.Duration(1) * time.Second) {
		if order.Status == "Filled" {
			break
		}
		order = GetOrderByOrderRef(orderID)
	}
}

func TestGetOrders(t *testing.T) {
	go KeepAlive()
	GetAllOrders()
}

func TestLive(t *testing.T) {
	InitializeServer()
	SubscribeLive()
}

func TestDeleteOrder(t *testing.T) {
	InitializeServer()
	order := Order{OrderID: 837322327}
	CancelOrder(order)
}
