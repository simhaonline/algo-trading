package main

import (
	"github.com/go-kit/kit/log/level"
	"bitbucket.org/garyyu/go-binance"
	"time"
	"fmt"
	"database/sql"
)

var (
	LatestTradeID = make(map[string]int64)
)

/*
 * Expensive API: Weight=5
 * 		Only call it when we don't know the OrderID, for example when import project.
 */
func QueryMyTrades(){

	ProjectMutex.Lock()
	defer ProjectMutex.Unlock()

	for _, aliveProject := range ActiveProjectList {

		if aliveProject.IsClosed {
			continue
		}

		oldLatestTradeID,ok := LatestTradeID[aliveProject.Symbol]
		if !ok{
			LatestTradeID[aliveProject.Symbol] = 0
			oldLatestTradeID = 0
		}
		keepOldOne := false

		myTrades, err := binanceSrv.MyTrades(binance.MyTradesRequest{
			Symbol:     aliveProject.Symbol,
			FromID: 	LatestTradeID[aliveProject.Symbol],
			RecvWindow: 5 * time.Second,
			Timestamp:  time.Now(),
		})
		if err != nil {
			level.Error(logger).Log("QueryMyTrades - fail! Symbol=", aliveProject.Symbol)
			return
		}
		//
		//fmt.Printf("QueryMyTrades - got %d trades\n", len(myTrades))

		var newTradesImported = 0
		for _, trade := range myTrades {

			if trade.ID > LatestTradeID[aliveProject.Symbol] {
				LatestTradeID[aliveProject.Symbol] = trade.ID
			}

			if GetTradeId(trade.ID)<=0 {
				if InsertTrade(aliveProject.Symbol, trade)<0 {
					level.Error(logger).Log("QueryMyTrades - InsertTrade fail! trade:", trade)
					keepOldOne = true
				}else{
					newTradesImported += 1
				}
			}
		}

		if newTradesImported == 0{
			continue
		}

		// in case we fail to save to local database
		if keepOldOne {
			LatestTradeID[aliveProject.Symbol] = oldLatestTradeID
		}

		// Get recent trades list in this asset, with the order of latest first.
		tradeList := getRecentTradeList(aliveProject.Symbol, binance.Day)
		amount := 0.0
		invest := 0.0

		tradesNum := 0
		for _,trade := range tradeList {
			tradesNum += 1

			if trade.IsBuyer {
				amount += trade.Qty
				invest += trade.Qty * trade.Price
			}else{
				amount -= trade.Qty
				invest -= trade.Qty * trade.Price
			}

			if FloatEquals(amount, aliveProject.InitialAmount) {
				// Finally! We find the trade(s) where this asset balance came from.
				aliveProject.InitialBalance = invest
				aliveProject.InitialPrice = invest / amount		// average price if multiple trades
				break
			}

			// handle special case: can't exactly match! just try our best.
			if amount > aliveProject.InitialAmount {
				// reverse the last one
				if trade.IsBuyer {
					amount -= trade.Qty
					invest -= trade.Qty * trade.Price
				}else{
					amount += trade.Qty
					invest += trade.Qty * trade.Price
				}
				aliveProject.InitialBalance = invest
				aliveProject.InitialPrice = invest / amount
				break
			}
		}

		// handle special case for 'BNBBTC'
		if aliveProject.Symbol=="BNBBTC" && aliveProject.InitialBalance<=0 {

			// just use all 'BNB' trades we found!
			aliveProject.InitialBalance = invest
			aliveProject.InitialPrice = invest / amount
		}

		fmt.Printf("QueryMyTrades - amount=%f, invest=%f. project id=%d, InitialBalance=%f\n",
			amount, invest, aliveProject.id, aliveProject.InitialBalance)

		// We find it? Let's put the ProjectID into all these trades
		if aliveProject.InitialBalance>0 {

			for i:=0; i<tradesNum; i++{
				trade := tradeList[i]
				trade.ProjectID = aliveProject.id

				if !UpdateTradeProjectID(&trade){
					fmt.Println("QueryMyTrades - UpdateTradeProjectID Failed. trade:", trade)
				}
			}

			if !UpdateProjectInitialBalance(aliveProject){
				fmt.Println("QueryMyTrades - Warning! Update Project InitialBalance into database Fail. aliveProject:",
					aliveProject)
			}
		}else{
			fmt.Println("QueryMyTrades - Warning! new project for asset", aliveProject.Symbol,
				"not found in my trades history! Project can't be managed.")
			continue
		}

	}

}

/*
 * Insert Trade data into Database
 */
func InsertTrade(symbol string, trade *binance.Trade) int64{

	query := `INSERT INTO trade_list (
				Symbol, TradeID, Price, Qty, Commission, 
				CommissionAsset, Time, IsBuyer, IsMaker, IsBestMatch, InsertTime
			  ) VALUES (?,?,?,?,?,?,?,?,?,?,NOW())`

	res, err := DBCon.Exec(query,
		symbol,
		trade.ID,
		trade.Price,
		trade.Qty,
		trade.Commission,
		trade.CommissionAsset,
		trade.Time,
		trade.IsBuyer,
		trade.IsMaker,
		trade.IsBestMatch,
	)

	if err != nil {
		level.Error(logger).Log("InsertTrade - DBCon.Exec", err)
		return -1
	}

	id, _ := res.LastInsertId()
	return id
}


/*
 * Used for detect if trade exist in local database
 */
func GetTradeId(TradeID int64) int64 {

	row := DBCon.QueryRow("SELECT id FROM trade_list WHERE TradeID=?", TradeID)

	var id int64 = -1
	err := row.Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		level.Error(logger).Log("GetTradeId - Scan Err:", err)
	}

	return id
}


/*
 * Update Trade ProjectID into Database
 */
func UpdateTradeProjectID(tradeData *TradeData) bool{
	//
	//fmt.Printf("UpdateTradeProjectID - ProjectID=%d for id:%d\n",
	//	tradeData.ProjectID, tradeData.id)

	query := `UPDATE trade_list SET ProjectID=? WHERE id=?`

	res, err := DBCon.Exec(query,
		tradeData.ProjectID,
		tradeData.id,
	)

	if err != nil {
		level.Error(logger).Log("DBCon.Exec", err)
		return false
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected>=0 {
		return true
	}else{
		return false
	}
}


/*
 * Get recent trades from local database for one asset
 */
func getRecentTradeList(symbol string, interval binance.Interval) []TradeData{

	tradeList := make([]TradeData, 0)

	query := "select * from trade_list where Symbol='" + symbol +
		"' and InsertTime > DATE_SUB(NOW(), INTERVAL "

	switch interval {
	case binance.ThreeDays:
		query += "3 DAY)"
	case binance.Week:
		query += "1 WEEK)"
	case binance.Month:
		query += "1 MONTH)"
	default:
		query += "1 DAY)"
	}

	query += " order by Time desc"

	rows, err := DBCon.Query(query)

	if err != nil {
		level.Error(logger).Log("getRecentTradeList - DBCon.Exec", err)
		panic(err.Error())
	}
	defer rows.Close()

	for rows.Next() {

		tradeData := TradeData{}

		err := rows.Scan(&tradeData.id,
			&tradeData.ProjectID, &tradeData.Symbol, &tradeData.TradeID,
			&tradeData.Price, &tradeData.Qty, &tradeData.Commission,
			&tradeData.CommissionAsset, &tradeData.Time, &tradeData.IsBuyer,
			&tradeData.IsMaker, &tradeData.IsBestMatch, &tradeData.InsertTime)

		if err != nil {
			level.Error(logger).Log("getRecentTradeList - Scan Err:", err)
			continue
		}

		tradeList = append(tradeList, tradeData)
	}

	return tradeList
}
