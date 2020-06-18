package ibkr

import (
	"sync"
	"time"
)

var LiveOrders = struct {
	Orders []Order
	Mut    sync.RWMutex
}{}

// UpdateTradeStatusesInterval ...
func UpdateTradeStatusesInterval(sec int) {
	for range time.Tick(time.Duration(sec) * time.Second) {
		LiveOrders.Mut.Lock()
		LiveOrders.Orders = GetAllOrders()
		LiveOrders.Mut.Unlock()
	}
}

// LiveGetOrderByOrderRef retrieve from LiveOrders variable
func LiveGetOrderByOrderRef(orderRef string) Order {
	LiveOrders.Mut.RLock()
	defer LiveOrders.Mut.RUnlock()
	orders := LiveOrders.Orders
	for i := range orders {
		order := orders[i]
		if order.Order_ref == orderRef {
			return order
		}
	}
	return Order{}
}
