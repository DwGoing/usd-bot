package app

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/DwGoing/usd-bot/internal/service"
	"github.com/alibaba/ioc-golang/extension/config"
)

// +ioc:autowire=true
// +ioc:autowire:type=singleton

type App struct {
	BinanceService      *service.BinanceService `singleton:""`
	Symbols             *config.ConfigSlice     `config:",symbols"`
	VolumPerTransaction *config.ConfigInt64     `config:",volumPerTransaction"`
	VolumMaximum        *config.ConfigInt64     `config:",volumMaximum"`

	binanceMessageCallback chan any
	symbols                []string
	volumPerTransaction    int64
	volumMaximum           int64
	balances               map[string]float64
}

func (app *App) Initialize() error {
	log.SetPrefix("[App]")
	callbackChannel := make(chan any, 1024)
	app.binanceMessageCallback = callbackChannel
	app.symbols = make([]string, len(app.Symbols.Value()))
	app.volumPerTransaction = app.VolumPerTransaction.Value()
	app.volumMaximum = app.VolumMaximum.Value()
	app.balances = make(map[string]float64)
	for i, v := range app.Symbols.Value() {
		arr := strings.Split(v.(string), "/")
		if len(arr) != 2 {
			log.Printf("invaild symbol: %s", v)
			continue
		}
		app.symbols[i] = ""
		for _, av := range arr {
			app.balances[av] = 0
			app.symbols[i] += av
		}
	}
	app.BinanceService.InitializeWebsocket()
	go func() {
		defer close(callbackChannel)
		for {
			response := <-app.binanceMessageCallback
			switch response := response.(type) {
			case service.AccountStatusResponse:
				app.updateBalance(response.Result.Balances)
				app.BinanceService.SendTickerPriceMessage(app.symbols, app.binanceMessageCallback)
			case service.TickerPriceResponse:
				app.trade(response.Result)
			case service.OrderPlaceResponse:
			}
		}
	}()

	app.BinanceService.SendAccountStatusMessage(app.binanceMessageCallback)
	return nil
}

func (app *App) updateBalance(balances []service.AccountStatusResultBalance) {
	for _, v := range balances {
		if _, ok := app.balances[v.Asset]; !ok {
			continue
		}
		balance, err := strconv.ParseFloat(v.Free, 64)
		if err != nil {
			return
		}
		app.balances[v.Asset] = balance
	}
	log.Printf("balances updated: %+v", app.balances)
}

func (app *App) trade(prices []service.TickerPriceResultItem) {
	log.Printf("%+v", prices)
	var symbol string
	var side string
	var heightPrice float64
	for token, balance := range app.balances {
		if int64(balance) < app.volumPerTransaction {
			continue
		}
		for _, v := range prices {
			if b, ok := app.balances[strings.ReplaceAll(v.Symbol, token, "")]; !ok || b > float64(app.volumMaximum) {
				continue
			}
			if strings.HasPrefix(v.Symbol, token) {
				price, err := strconv.ParseFloat(v.Price, 64)
				if err != nil {
					continue
				}
				if price > heightPrice {
					symbol = v.Symbol
					side = "SELL"
					heightPrice = price
				}
			} else if strings.HasSuffix(v.Symbol, token) {
				price, err := strconv.ParseFloat(v.Price, 64)
				if err != nil {
					continue
				}
				price = 1 / price
				if price > heightPrice {
					symbol = v.Symbol
					side = "BUY"
					heightPrice = price
				}
			} else {
				continue
			}
		}
	}
	if heightPrice > 1.0005 {
		log.Printf("%s price: %f", symbol, heightPrice)
		app.BinanceService.SendOrderPlaceMessage(symbol, side, "MARKET", app.volumPerTransaction, app.binanceMessageCallback)
		time.Sleep(time.Second * 60)
		app.BinanceService.SendAccountStatusMessage(app.binanceMessageCallback)
	} else {
		time.Sleep(time.Second * 10)
		app.BinanceService.SendTickerPriceMessage(app.symbols, app.binanceMessageCallback)
	}
}
