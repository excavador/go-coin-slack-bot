/*

mybot - Illustrative Slack bot in Go

Copyright (c) 2015 RapidLoop

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sort"
	"strconv"
	"time"
	"context"
	"os/signal"
	"syscall"
	"sync"
)

func NewCoinMarker() TickersPipeline {
	return &coinMarket{}
}

var mkt TickersPipeline

func init() {
	mkt = NewCoinMarker()
}

func LogError(err error) {
	fmt.Println(err)
}


func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mybot slack-bot-token")
		os.Exit(1)
	}

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	var wait sync.WaitGroup

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-signalChannel
		cancel()
	}()

		// start a websocket-based Real Time API session
	ws, id := slackConnect(os.Args[1])
	fmt.Println("My ID: ", id)
	fmt.Println("mybot ready, ^C exits")

	helpF := func(m Message) {
		m.Text = "Commands: \n" +
			"*coin <symbol symbol ...>* - get coin stats \n" +
			"*rank <n>* - get top N records sorted by price\n" +
			"*watch symbol threshold* - watch coin price change\n" +
			"*unwatch symbol* - unwatch coin\n" +
			"*watchlist* - list current watched coins"
		postMessage(ws, m)
	}

	wl := NewWatchList(ctx)


	msgF := func(m Message) {
		recv := func() {
			if r := recover(); r != nil {
				go helpF(m)
			}
		}
		defer recv()
		if m.Type == "message" && strings.HasPrefix(m.Text, "<@"+id+">") {
			// if so try to parse if
			parts := strings.Fields(m.Text)
			switch parts[1] {
			case "watch":
				if len(parts) != 4 {
					helpF(m)
				} else if threshold, err := strconv.Atoi(parts[3]); err == nil && threshold > 0 {
					symbol := strings.ToLower(parts[2])
					if tdl, err := mkt.FetchCoins(ctx, ""); err == nil {
						var td *TickerData
						for _, current := range tdl {
							if strings.ToLower(string(td.Symbol)) == symbol {
								td = &current
								break
							}
						}
						if td != nil {
							wl.Watch(ctx, m.Channel, threshold, td)
						} else {
							m.Text = "Coin '" + symbol + "' does not exist"
							postMessage(ws, m)
						}
					} else {
						m.Text = "Cannot fetch coins"
						postMessage(ws, m)
					}
				} else {
					m.Text = "Cannot parse threshold " + parts[3]
					postMessage(ws, m)
				}
			case "unwatch":
				wl.UnWatch(ctx, parts[2])
			case "watchlist":
				var sl []string
				wl.Foreach(ctx, func(symbol string, watch Watch) {
					sl = append(sl, string(symbol))
				})
				m.Text = fmt.Sprintf("Tickers: [%s]", strings.Join(sl, " "))
				postMessage(ws, m)
			case "coin":
				// looks good, get the quote and reply with the result
				go func(m Message) {
					defer recv()
					m.Text = getQuote(ctx, parts[2:])
					postMessage(ws, m)
				}(m)
			case "rank":
				r, err := strconv.Atoi(parts[2])
				if err != nil {
					r = 10
				}
				go func(m Message) {
					m.Text = rankCoins(ctx, r)
					postMessage(ws, m)
				}(m)
			default:
				go helpF(m)
			}
			// NOTE: the Message object is copied, this is intentional
		}
	}

	wait.Add(1)
	go func() {
		defer wait.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Fatal(r)
			}
		}()
		for {
			if m, err := getMessage(ws); err != nil {
				LogError(err)
				cancel()
			} else {
				msgF(m)
			}
		}
	}()

	ticker := time.NewTicker(30 * time.Second)

	wait.Add(1)
	go func () {
		defer wait.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Fatal(r)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sl := make(map[string]Watch)
				wl.Foreach(ctx, func(symbol string, watch Watch) {
					sl[symbol] = watch
				})
				for symbol, watch := range sl {
					fmt.Printf("Check %1s : %2s\n", symbol, watch.Name)
					var td []TickerData
					var err error
					if td, err = mkt.FetchCoins(ctx, symbol); err != nil {
						fmt.Printf("Skipping %1s\n", symbol)
						return
					}
					if len(td) == 0 {
						continue
					}
					if message := wl.Update(ctx, &td[0]); message != nil {
						postMessage(ws, *message)
					}
				}
			}

		}
	}()
	<-ctx.Done()
}

// Get the quote via Yahoo. You should replace this method to something
// relevant to your team!
func getQuote(ctx context.Context, sym []string) string {

	set := make(map[string]bool)
	for _, v := range sym {
		set[strings.ToLower(v)] = true
	}

	coins, err := mkt.FetchCoins(ctx, "")
	if err == nil && coins != nil && len(coins) > 0 {
		var resp string
		found := false
		for _, coin := range coins {
			if _, ok := set[strings.ToLower(coin.Symbol)]; ok {
				found = true
				resp = resp + fmt.Sprintf("%1s $%2.2f, update 1H: %3.2f%%, update 24H: %4.2f%%\n",
					coin.Symbol, coin.PriceUSD, coin.PercentChange1H, coin.PercentChange24H)
			}
		}
		if found {
			return resp
		} else {
			return fmt.Sprintf("'%1v' not found", sym)
		}
	} else {
		return "n/a"
	}

}

func rankCoins(ctx context.Context, limit int) string {
	min := func(l, r int) int {
		if l < r {
			return l
		} else {
			return r
		}
	}

	coins, err := mkt.FetchCoins(ctx,"")
	if err == nil && coins != nil && len(coins) > 0 {
		var resp string
		sort.Sort(Sortable(coins))
		for _, v := range coins[:min(limit, 30)] {
			resp = resp + fmt.Sprintf("%1s : %2s => price: $%3.2f : market: $%4.2f\n", v.Name, v.Symbol, v.PriceUSD, v.MarketCapUSD)
		}
		return resp
	} else {
		return "n/a"
	}

}

type Sortable []TickerData

func (s Sortable) Less(i, j int) bool {
	return (s)[i].MarketCapUSD > (s)[j].MarketCapUSD
}

func (s Sortable) Len() int {
	return len(s)
}

func (s Sortable) Swap(i, j int) {
	tmp := (s)[i]
	(s)[i] = (s)[j]
	(s)[j] = tmp
}
