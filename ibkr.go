package ibkr

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/beats/x-pack/elastic-agent/pkg/agent/errors"
	"github.com/gorilla/websocket"
)

/*
cd clientportal.gw
bash bin/run.sh root/conf.yaml
Login: https://localhost:5000	/
https://interactivebrokers.github.io/cpwebapi/swagger-ui.html
*/

type ConID struct {
	ID  int64
	Mkt string
}

type Order struct {
	Acct              string
	ConID             int
	OrderDesc         string
	Description1      string
	Ticker            string
	SecType           string
	ListingExchange   string
	RemainingQuantity float32
	FilledQuantity    float64
	CompanyName       string
	Status            string
	OrigOrderType     string
	Side              string
	AvgPrice          string // this is closed price
	BgColor           string
	FgColor           string
	OrderID           int
	ParentID          int
	Order_ref         string
	price             float64 // this is submitted price
}

var conidPath = "conid.json"
var conids = map[string]ConID{}

var tr = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}
var client = &http.Client{Transport: tr}

var SecType = struct {
	STK string
	FUT string
}{
	STK: "STK",
	FUT: "FUT",
}

var OrderType = struct { // MKT (Market), LMT (Limit), STP (Stop) or STP_LIMIT (stop limit)
	MKT       string
	LMT       string
	STP       string
	STP_LIMIT string
}{
	MKT:       "MKT",
	LMT:       "LMT",
	STP:       "STP",
	STP_LIMIT: "STP_LIMIT",
}

var OrderSide = struct {
	BUY  string
	SELL string
}{
	BUY:  "BUY",
	SELL: "SELL",
}

var Tif = struct {
	GTC string
	DAY string
}{
	GTC: "GTC",
	DAY: "DAY",
}

func InitializeServer() {
	loadConIDs()
	err := CheckAuthenticated()
	if err != nil {
		panic(err)
	}
	loadConIDs()
	go KeepAlive()
	go UpdateTradeStatusesInterval(5)
}

func CheckAuthenticated() error { // very important!
	w := strings.NewReader("")
	CallAccounts()
	req, err := http.NewRequest("GET", "https://localhost:5000/v1/portal/iserver/auth/status", w)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return errors.New(resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	var qq map[string]interface{}
	err = json.Unmarshal(body, &qq)

	if err != nil {
		return err
	}
	authenticated := qq["authenticated"].(bool)
	if !authenticated {
		println("Not authenticated. Retrying...")
		req, _ := http.NewRequest("GET", "https://localhost:5000/v1/portal/iserver/reauthenticate", strings.NewReader(""))
		resp, err = client.Do(req)
		body, _ = ioutil.ReadAll(resp.Body)
		println(string(body))
		CallAccounts()
		req, _ = http.NewRequest("GET", "https://localhost:5000/v1/portal/iserver/account/orders ", strings.NewReader(""))
		resp, err = client.Do(req)
		body, _ = ioutil.ReadAll(resp.Body)
		println(string(body))
		CheckAuthenticated()
	}
	println("Authenticated")

	return nil
}

func KeepAlive() {
	w := strings.NewReader("")
	for range time.Tick(time.Duration(10) * time.Second) {
		req, _ := http.NewRequest("GET", "https://localhost:5000/v1/portal/sso/validate", w)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := ioutil.ReadAll(resp.Body)
		if string(body) == "" {
			panic("Server error")
		}
		println(string(body))
		CheckAuthenticated()
	}
}

func GetExecutedTradesForPast6Days() {
	//https://localhost:5000/v1/portal/iserver/account/trades
}

func GetContractIDSearchResults(sym string) ConID { //need to populate this...
	//POST https://localhost:5000/v1/portal/iserver/secdef/search
	/*
		{
			"symbol": "AAPL",
			"name": false,
			"secType": "STK"
		}
	*/
	w := strings.NewReader("{\"symbol\":\"" + sym + "\",\"name\":false,\"secType\":\"STK\"}")
	req, err := http.NewRequest("POST", "https://localhost:5000/v1/portal/iserver/secdef/search", w)
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := ioutil.ReadAll(res.Body)
	var qq []map[string]interface{}
	err = json.Unmarshal(body, &qq)
	if err != nil {
		if strings.Contains(string(body), "No symbol found") {
			println(sym + ": No symbol found")
			return ConID{}
		} else {
			println(string(body))
			panic(err)
		}
	}
	stacked := " :"
	for q := range qq {
		if _, e := qq[q]["description"]; e {
			switch qq[q]["description"].(type) {
			case string:
				desc := qq[q]["description"].(string)
				if desc == "NYSE" || desc == "NASDAQ" || desc == "ARCA" ||
					desc == "PINK" || desc == "AMEX" || desc == "BATS" || desc == "MEXI" {
					// VALUE, VENTURE not added
					return ConID{
						ID:  int64(qq[q]["conid"].(float64)),
						Mkt: desc,
					}
				} else {
					stacked += desc + ","
				}
			case nil:
				println(sym)
			}

		}
	}
	println(sym, stacked)
	return ConID{}
}

func PlaceOrder(sym, secType, orderType, orderSide, tif string, price float64, quantity int) (orderRef string, order Order, err error) {
	// returns immediate orderStatus: Filled (for MKT), Submitted
	// STEP 1: POST https://localhost:5000/v1/portal/iserver/account/U3818550/order
	/*
		{
		  "conid": 4730124,
		  "secType": "STK",
		  "orderType": "LMT",
		  "outsideRTH": false,
		  "price": 1,
		  "side": "BUY",
		  "tif": "DAY",
		  "quantity": 1,
		  "useAdaptive": true
		}
	*/
	conid := conids[sym].ID
	if conid == 0 {
		println("conid not found.")
		return "", Order{}, errors.New("conid not found.")
	}
	t := time.Now().Unix()
	orderRef = sym + strconv.FormatInt(t, 10)
	b1 := "{" +
		"  \"conid\": " + strconv.FormatInt(conid, 10) + "," +
		"  \"secType\": \"" + secType + "\"," +
		"  \"orderType\": \"" + orderType + "\"," +
		"  \"cOID\": \"" + orderRef + "\"," +
		"  \"outsideRTH\": false,"
	b2 := "  \"side\": \"" + orderSide + "\"," +
		"  \"tif\": \"" + tif + "\"," +
		"  \"quantity\": " + strconv.Itoa(quantity) + "," +
		"  \"useAdaptive\": true" +
		"}"
	body := b1 + "  \"price\": " + fmt.Sprintf("%f", price) + "," + b2
	if orderType == OrderType.MKT {
		body = b1 + b2
	}

	w := strings.NewReader(body)
	req, err := http.NewRequest("POST", "https://localhost:5000/v1/portal/iserver/account/U3818550/order", w)
	req.Header.Add("Content-Type", "application/json")
	if err != nil {
		panic(err)
	}
	_, err = autoOrderSubmitter(req)
	if err != nil {
		return "", Order{}, err
	}
	order = GetOrderByOrderRef(orderRef)
	return orderRef, order, nil
}

func CancelOrder(o Order) {
	req, _ := http.NewRequest("DELETE", "https://localhost:5000/v1/portal/iserver/account/U3818550/order/"+strconv.Itoa(o.OrderID), strings.NewReader(""))
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	rr, _ := ioutil.ReadAll(res.Body)
	println(string(rr))
}

func GetAllOrders() []Order {
	w := strings.NewReader("")
	req, _ := http.NewRequest("GET", "https://localhost:5000/v1/portal/iserver/account/orders", w)
	req.Header.Add("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	rbody, _ := ioutil.ReadAll(res.Body)
	var qq = struct {
		Orders        []Order
		Notifications []interface{}
	}{}

	err = json.Unmarshal(rbody, &qq)
	if err != nil {
		panic(err.Error())
	}
	orders := qq.Orders
	return orders
}

func GetLast7DTrades() {
	//https://localhost:5000/v1/portal/iserver/account/trades
}

func GetOrderByOrderRef(orderRef string) Order {
	orders := GetAllOrders()
	for i := range orders {
		order := orders[i]
		if order.Order_ref == orderRef {
			return order
		}
	}
	return Order{}
}

func autoOrderSubmitter(req *http.Request) (string, error) {
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	rbody, _ := ioutil.ReadAll(res.Body)
	var qq []map[string]interface{}
	err = json.Unmarshal(rbody, &qq)
	if err != nil {
		panic(string(rbody))
	}
	qnid := qq[0]["id"].(string)
	msg := qq[0]["message"].([]interface{})[0].(string)
	println(msg)
	if strings.Contains(msg, "Are you sure you want to submit this order?") {
		req, err = http.NewRequest("POST", "https://localhost:5000/v1/portal/iserver/reply/"+qnid, strings.NewReader("{\"confirmed\":true}"))
		req.Header.Add("Content-Type", "application/json")
		return autoOrderSubmitter(req)
	} else if strings.Contains(msg, "Confirm Mandatory Cap Price") || strings.Contains(msg, "Market Order Confirmation") {
		/*
			[
				{
						"order_id": "204858507",
						"local_order_id": "xxx",
						"order_status": "Submitted"
				}
			]
		*/
		req, err = http.NewRequest("POST", "https://localhost:5000/v1/portal/iserver/reply/"+qnid, strings.NewReader("{\"confirmed\":true}"))
		req.Header.Add("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		rbody, _ := ioutil.ReadAll(res.Body)
		println(string(rbody))
		var qq []map[string]interface{}
		err = json.Unmarshal(rbody, &qq)
		if err != nil {
			panic(string(rbody))
		}
		orderID := qq[0]["order_id"].(string)
		return orderID, nil
	}
	return "", err
}

func GetPositionBySym(sym string) {
	// By se
	//https://localhost:5000/v1/portal/portfolio/U3818550/position/4730124
}

func GetPortfolioPositions() {
	//https://localhost:5000/v1/portal/portfolio/U3818550/positions/0
	/*
		[
		    {
		        "acctId": "U3818550",
		        "conid": 4730124,
		        "contractDesc": "ENB",
		        "assetClass": "STK",
		        "position": 100.0,
		        "mktPrice": 32.3199997,
		        "mktValue": 3232.0,
		        "currency": "USD",
		        "avgCost": 31.369,
		        "avgPrice": 31.369,
		        "realizedPnl": 0.0,
		        "unrealizedPnl": 95.1,
		        "exchs": null,
		        "expiry": null,
		        "putOrCall": null,
		        "multiplier": null,
		        "strike": 0.0,
		        "exerciseStyle": null,
		        "undConid": 0,
		        "conExchMap": [],
		        "model": ""
		    }
		]
	*/
}

func initializeConIDs() {
	println("Init ConIDs...")
	indices := []string{"AAL"} //datahandler.GetMinIndices()
	loadConIDs()
	for i := range indices {
		if i%100 == 0 {
			println("Done ", i, " of ", len(indices))
			saveConIDs()
		}
		ix := indices[i]
		if _, e := conids[ix]; !e {
			conids[ix] = GetContractIDSearchResults(ix)
		}
	}
	saveConIDs()
}

func loadConIDs() {
	x, _ := ioutil.ReadFile(conidPath)
	err := json.Unmarshal(x, &conids)
	if err != nil {
		panic(err)
	}
}

func saveConIDs() {
	data, err := json.Marshal(conids)
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile(conidPath, data, 0644)
	if err != nil {
		panic(err)
	}
}

func SubscribeLive() {
	/*
		Bugged*
		Hello, apologies for the delay. I see others have reported the same issue with the streaming web socket and the CP API. Our development group is currently looking into this issue
		In the meanwhile you may use the /snapshot endpoint for marekt data
	*/
	u := url.URL{Scheme: "wss", Host: *flag.String("addr", "localhost:5000", "http service address"), Path: "demo/v1/portal/ws"}
	log.Printf("connecting to %s", u.String())

	header := http.Header{}

	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	c, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	body := []byte("s+md+265598")
	c.WriteMessage(websocket.TextMessage, body)

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		log.Printf("recv: %s", message)
	}
}

func SubscribeLive2() {
	// ********WARNING******** This costs USD0.01 per snapshot!!
	// Uses snapshot, https://investors.interactivebrokers.com/api/doc.html#tag/Market-Data/paths/~1iserver~1marketdata~1snapshot/get
	// curl -k -X GET "https://localhost:5000/v1/portal/iserver/marketdata/snapshot?conids=265598%2C107113386&fields=31%2C55" -H  "accept: application/json"
	/*
		Field names
			31,string,Last Price
			55,string,Symbol
			58,string,Text
			70,string,High
			71,string,Low
			72,string,Position
			73,string,Market Value
			74,string,Average Price
			75,string,Unrealized PnL
			76,string,Formatted position
			77,string,Formatted Unrealized PnL
			78,string,Daily PnL
			82,string,Change Price
			83,string,Change Percent
			84,string,Bid Price
			85,string,Ask Size
			86,string,Ask Price
			87,string,Volume
			88,string,Bid Size
			6004,string,Exchange
			6008,string,Conid
	*/
}

func CallAccounts() {
	req, _ := http.NewRequest("GET", "https://localhost:5000/v1/portal/iserver/accounts", strings.NewReader(""))
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := ioutil.ReadAll(resp.Body)
	println(string(body))
}
