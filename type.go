package main

import (
	"sync"
	"context"
	"fmt"
	"io/ioutil"
	"encoding/json"
	"net/http"
	"net/url"
	"math"
)

type (
	Message struct {
		Id      uint64 `json:"id"`
		Type    string `json:"type"`
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}

	Watch struct {
		Channel   string
		Name      string
		LastPrice float64
		Threshold int
	}

	WatchList struct {
		mutex  sync.Mutex
		list   map[string]*Watch
	}

	TickerData struct {
		Id               string  `json:"id"`
		Name             string  `json:"name"`
		Symbol           string  `json:"symbol"`
		Rank             int16    `json:"rank,string"`
		PriceUSD         float64 `json:"price_usd,string"`
		PriceBTC         float64 `json:"price_btc,string"`
		Volume24H        float64 `json:"24h_volume_usd,string"`
		MarketCapUSD     float64 `json:"market_cap_usd,string"`
		AvailableSupply  float64 `json:"available_supply,string"`
		TotalSupply      float64 `json:"total_supply,string"`
		PercentChange1H  float32 `json:"percent_change_1h,string"`
		PercentChange24H float32 `json:"percent_change_24h,string"`
		PercentChange7D  float32 `json:"percent_change_7d,string"`
		LastUpdated      int32   `json:"last_updated,string"`
	}

	TickersPipeline interface {
		FetchCoins(ctx context.Context, symbol string) ([]TickerData, error)
	}

	coinMarket struct{}
)

func (mkt *coinMarket) FetchCoins(ctx context.Context, coinCode string) (result []TickerData, err error) {
	urlraw := "https://api.coinmarketcap.com/v1/ticker"
	if coinCode != "" {
		urlraw = fmt.Sprintf("%s/%s", urlraw, coinCode)
	}

	var request *http.Request
	request.URL, _ = url.Parse(urlraw)
	request = request.WithContext(ctx)

	client := &http.Client{}

	var resp *http.Response
	if resp, err = client.Do(request); err != nil {
		LogError(err)
		return
	}
	defer resp.Body.Close()
	var body []byte
	if body, err = ioutil.ReadAll(resp.Body); err != nil {
		LogError(err)
		return
	}
	if err = json.Unmarshal(body, &result); err != nil {
		LogError(err)
		return
	}
	return
}


func NewWatchList(ctx context.Context) (*WatchList) {
	return &WatchList{
		list: make(map[string]*Watch),
	}
}

func (wl *WatchList) update(ctx context.Context, td *TickerData) *Message {
	watch := wl.list[td.Symbol]
	delta := td.PriceUSD - td.PriceUSD
	watch.LastPrice = td.PriceUSD
	if math.Abs(delta) > float64(watch.Threshold) {
		return &Message{
			Channel: watch.Channel,
			Type: "message",
			Text: fmt.Sprintf("[ %1s ] $%+2.2f to $%3.2f", td.Symbol, delta, td.PriceUSD),
		}
	} else {
		return nil
	}
}

func (wl *WatchList) Update(ctx context.Context, td *TickerData) *Message {
	if td == nil {
		return nil
	}
	wl.mutex.Lock()
	defer wl.mutex.Unlock()
	return wl.update(ctx, td)
}

func (wl *WatchList) Foreach(ctx context.Context, action func(symbol string, watch Watch)) {
	wl.mutex.Lock()
	defer wl.mutex.Unlock()
	for symbol, watch := range wl.list {
		action(symbol, *watch)
	}
}

func (wl *WatchList) Watch(ctx context.Context, channel string, threshold int, td *TickerData) *Message {
	if td == nil {
		return nil
	}
	wl.mutex.Lock()
	defer wl.mutex.Unlock()
	wl.list[td.Symbol] = &Watch{
		Channel: channel,
		Name: td.Name,
		Threshold: threshold,
	}
	return wl.update(ctx, td)
}

func (wl *WatchList) UnWatch(ctx context.Context, symbol string) {
	wl.mutex.Lock()
	defer wl.mutex.Unlock()
	delete(wl.list, symbol)
}