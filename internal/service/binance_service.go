package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/alibaba/ioc-golang/extension/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// +ioc:autowire=true
// +ioc:autowire:type=singleton
// +ioc:autowire:constructFunc=NewBinanceService

type BinanceService struct {
	WebsocketUrl *config.ConfigString `config:",binance.websocketUrl"`
	ApiKey       *config.ConfigString `config:",binance.apiKey"`
	SecretKey    *config.ConfigString `config:",binance.secretKey"`

	connection *websocket.Conn
	requests   map[string]any
}

func NewBinanceService(service *BinanceService) (*BinanceService, error) {
	service.requests = make(map[string]any)
	return service, nil
}

func (service *BinanceService) InitializeWebsocket() error {
	conn, _, err := websocket.DefaultDialer.Dial(service.WebsocketUrl.Value(), nil)
	if err != nil {
		log.Printf("初始化websocket异常: %s", err)
		return err
	}
	service.connection = conn
	go func() {
		defer conn.Close()
		for {
			err := service.receiveMessage()
			if err != nil {
				log.Printf("接收消息异常: %s", err)
				// 断线重连
				err = service.InitializeWebsocket()
				for err != nil {
					time.Sleep(time.Second * 3)
					err = service.InitializeWebsocket()
				}
				break
			}
		}
	}()
	// health check
	// go func() {
	// 	for {
	// 		err := service.SendPingMessage()
	// 		if err != nil {
	// 			log.Printf("ping error: %s", err)
	// 			time.Sleep(time.Second * 3)
	// 			service.InitializeWebsocket()
	// 			return
	// 		}
	// 		time.Sleep(time.Second * 30)
	// 	}
	// }()
	return nil
}

func (service *BinanceService) receiveMessage() error {
	_, msgBytes, err := service.connection.ReadMessage()
	if err != nil {
		return err
	}
	// log.Printf("接收消息: %s", msgBytes)
	var response Response
	err = json.Unmarshal(msgBytes, &response)
	if err != nil {
		return err
	}
	request, ok := service.requests[response.Id]
	if !ok {
		return nil
	}
	switch request := request.(type) {
	case AccountStatusRequest:
		var response AccountStatusResponse
		err = json.Unmarshal(msgBytes, &response)
		if err != nil {
			return nil
		}
		if request.Callback == nil {
			return nil
		}
		request.Callback <- response
	case TickerPriceRequest:
		var response TickerPriceResponse
		err = json.Unmarshal(msgBytes, &response)
		if err != nil {
			return nil
		}
		if request.Callback == nil {
			return nil
		}
		request.Callback <- response
	case OrderPlaceRequest:
		var response OrderPlaceResponse
		err = json.Unmarshal(msgBytes, &response)
		if err != nil {
			return nil
		}
		if request.Callback == nil {
			return nil
		}
		request.Callback <- response
	}
	delete(service.requests, response.Id)
	return nil
}

func (service *BinanceService) sendMessage(id string, request any) error {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return err
	}
	// log.Printf("发送消息: %s", requestBytes)
	err = service.connection.WriteMessage(1, requestBytes)
	if err != nil {
		return err
	}
	service.requests[id] = request
	return nil
}

func sign(data string, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

type Request struct {
	Id       string   `json:"id"`
	Method   string   `json:"method"`
	Callback chan any `json:"-"`
}

func (service *BinanceService) NewRequest(method string, callback chan any) Request {
	return Request{Id: uuid.NewString(), Method: method, Callback: callback}
}

type Response struct {
	Id         string      `json:"id"`
	Status     int         `json:"status"`
	Error      Error       `json:"error"`
	RateLimits []RateLimit `json:"rateLimits"`
}

type Error struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type RateLimit struct {
	RateLimitType string `json:"rateLimitType"`
	Interval      string `json:"interval"`
	IntervalNum   int    `json:"intervalNum"`
	Limit         int    `json:"limit"`
	Count         int    `json:"count"`
}

func (service *BinanceService) SendPingMessage() error {
	request := service.NewRequest("ping", nil)
	err := service.sendMessage(request.Id, request)
	if err != nil {
		return err
	}
	return nil
}

type AccountStatusRequest struct {
	Request
	Params interface{} `json:"params"`
}

type AccountStatusParams struct {
	ApiKey    string `json:"apiKey"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
}

func (service *BinanceService) NewAccountStatusRequest(callback chan any) AccountStatusRequest {
	apiKey := string(*service.ApiKey)
	secretKey := string(*service.SecretKey)
	timestamp := time.Now().UnixMilli()
	signature := sign(fmt.Sprintf("apiKey=%s&timestamp=%d", apiKey, timestamp), secretKey)
	return AccountStatusRequest{
		Request: service.NewRequest("account.status", callback),
		Params:  AccountStatusParams{ApiKey: apiKey, Signature: signature, Timestamp: timestamp},
	}
}

type AccountStatusResponse struct {
	Response
	Result AccountStatusResult `json:"result"`
}

type AccountStatusResult struct {
	Balances []AccountStatusResultBalance `json:"balances"`
}

type AccountStatusResultBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

func (service *BinanceService) SendAccountStatusMessage(callback chan any) error {
	request := service.NewAccountStatusRequest(callback)
	err := service.sendMessage(request.Id, request)
	if err != nil {
		return err
	}
	return nil
}

type TickerPriceRequest struct {
	Request
	Params interface{} `json:"params"`
}

type TickerPriceParams struct {
	Symbols interface{} `json:"symbols"`
}

func (service *BinanceService) NewTickerPriceRequest(symbols []string, callback chan any) TickerPriceRequest {
	return TickerPriceRequest{
		Request: service.NewRequest("ticker.price", callback),
		Params:  TickerPriceParams{Symbols: symbols},
	}
}

type TickerPriceResponse struct {
	Response
	Result []TickerPriceResultItem `json:"result"`
}

type TickerPriceResultItem struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

func (service *BinanceService) SendTickerPriceMessage(symbols []string, callback chan any) error {
	request := service.NewTickerPriceRequest(symbols, callback)
	err := service.sendMessage(request.Id, request)
	if err != nil {
		return err
	}
	return nil
}

type OrderPlaceRequest struct {
	Request
	Params interface{} `json:"params"`
}

type OrderPlaceParams struct {
	ApiKey        string `json:"apiKey"`
	Signature     string `json:"signature"`
	Timestamp     int64  `json:"timestamp"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	QuoteOrderQty int64  `json:"quoteOrderQty"`
}

func (service *BinanceService) NewOrderPlaceRequest(symbol string, side string, oType string, quoteOrderQty int64, callback chan any) OrderPlaceRequest {
	apiKey := string(*service.ApiKey)
	secretKey := string(*service.SecretKey)
	timestamp := time.Now().UnixMilli()
	signature := sign(fmt.Sprintf("apiKey=%s&quoteOrderQty=%d&side=%s&symbol=%s&timestamp=%d&type=%s", apiKey, quoteOrderQty, side, symbol, timestamp, oType), secretKey)
	return OrderPlaceRequest{
		Request: service.NewRequest("order.place", callback),
		Params:  OrderPlaceParams{ApiKey: apiKey, Signature: signature, Timestamp: timestamp, Symbol: symbol, Side: side, Type: oType, QuoteOrderQty: quoteOrderQty},
	}
}

type OrderPlaceResponse struct {
	Response
	Result OrderPlaceResult `json:"result"`
}

type OrderPlaceResult struct {
	OrderId string `json:"orderId"`
	Price   string `json:"price"`
}

func (service *BinanceService) SendOrderPlaceMessage(symbol string, side string, oType string, quoteOrderQty int64, callback chan any) error {
	request := service.NewOrderPlaceRequest(symbol, side, oType, quoteOrderQty, callback)
	err := service.sendMessage(request.Id, request)
	if err != nil {
		return err
	}
	return nil
}
