// Package main implements a Binance futures trading bot that manages positions
// with automated stop-loss and take-profit orders based on configurable criteria.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	binance "github.com/adshao/go-binance/v2/futures"
	"github.com/joho/godotenv"
)

// Configuration defaults for the trading bot.
const (
	defaultSLPercentVal = 1.0
	defaultTPPercentVal = 3.0
	defaultSLFixedVal   = true
)

// Config holds application configuration loaded from environment.
type Config struct {
	DefaultSLPercent float64
	TPPercent        float64
	SLFixed          bool
	// Add other configuration values here
}

// SymbolPrecision stores price and quantity precision information for a trading symbol.
type SymbolPrecision struct {
	PricePrecision    int
	QuantityPrecision int
}

// PositionData contains all calculated data for a futures position.
type PositionData struct {
	Symbol           string
	PositionSide     string
	EntryPrice       float64
	MarkPrice        float64
	PositionAmt      float64
	AbsAmt           float64
	Leverage         float64
	IsLong           bool
	IsShort          bool
	CurrentProfitPct float64
	RawProfitPct     float64
	StopPrice        float64
	TakePrice        float64
	Quantity         string
	StopPriceStr     string
	TakePriceStr     string
	CurrentSLPct     float64
	RawSLPct         float64
	LeveragedSLPct   float64
	RawTPPct         float64
	LeveragedTPPct   float64
	PotentialProfit  float64
	PotentialLoss    float64
	RiskReward       float64
}

// StopLossLevel defines a profit threshold and corresponding stop-loss level.
type StopLossLevel struct {
	ProfitThreshold float64 // Profit threshold
	StopLossValue   float64 // Corresponding stop-loss level
}

// TradingService handles all trading operations.
type TradingService struct {
	client     *binance.Client
	config     Config
	symbolInfo map[string]SymbolPrecision
	stopLevels []StopLossLevel
}

// NewTradingService creates and initializes a new trading service.
func NewTradingService(client *binance.Client, config Config) (*TradingService, error) {
	// Initialize stop-loss levels
	stopLevels := []StopLossLevel{
		{150, 0}, {300, 100}, {450, 200}, {600, 300},
		{750, 400}, {900, 500}, {1050, 600}, {1200, 700},
		{1350, 800}, {1500, 900},
	}

	// Get symbol precision information
	symbolInfo, err := getSymbolPrecisions(client)
	if err != nil {
		return nil, fmt.Errorf("error getting exchange information: %w", err)
	}

	return &TradingService{
		client:     client,
		config:     config,
		symbolInfo: symbolInfo,
		stopLevels: stopLevels,
	}, nil
}

// loadConfig loads configuration from environment variables with defaults.
func loadConfig() Config {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: Error loading .env file: %v", err)
	}

	config := Config{
		DefaultSLPercent: defaultSLPercentVal,
		TPPercent:        defaultTPPercentVal,
		SLFixed:          defaultSLFixedVal,
	}

	// Override with environment variables if present
	if slStr := os.Getenv("DEFAULT_SL_PERCENT"); slStr != "" {
		if val, err := strconv.ParseFloat(slStr, 64); err == nil {
			config.DefaultSLPercent = val
		}
	}

	if tpStr := os.Getenv("TP_PERCENT"); tpStr != "" {
		if val, err := strconv.ParseFloat(tpStr, 64); err == nil {
			config.TPPercent = val
		}
	}

	if slFixedStr := os.Getenv("SL_FIXED"); slFixedStr != "" {
		if val, err := strconv.ParseBool(slFixedStr); err == nil {
			config.SLFixed = val
		}
	}

	return config
}

// sendTelegramMessage sends a notification to the configured Telegram chat.
func sendTelegramMessage(message string) error {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")

	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram configuration missing")
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {chatID},
		"text":    {message},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned error code: %d", resp.StatusCode)
	}
	return nil
}

// setupBinanceClient initializes and validates the Binance API client.
func setupBinanceClient() (*binance.Client, error) {
	apiKey := os.Getenv("BINANCE_API_KEY")
	apiSecret := os.Getenv("BINANCE_API_SECRET")

	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("binance API credentials not configured")
	}

	client := binance.NewClient(apiKey, apiSecret)

	// Validate API connection
	_, err := client.NewGetAccountService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Binance API: %w", err)
	}

	return client, nil
}

// getSymbolPrecisions retrieves precision information for all trading symbols.
func getSymbolPrecisions(client *binance.Client) (map[string]SymbolPrecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	exchangeInfo, err := client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, err
	}

	symbolInfo := make(map[string]SymbolPrecision, len(exchangeInfo.Symbols))
	for _, info := range exchangeInfo.Symbols {
		symbolInfo[info.Symbol] = SymbolPrecision{
			PricePrecision:    info.PricePrecision,
			QuantityPrecision: info.QuantityPrecision,
		}
	}
	return symbolInfo, nil
}

// calculateStopLoss determines the stop-loss price based on current profit levels.
func (ts *TradingService) calculateStopLoss(data *PositionData) float64 {
	// Determine current stop-loss percentage based on profit levels
	currentSLPct := ts.config.DefaultSLPercent
	for _, level := range ts.stopLevels {
		if data.CurrentProfitPct >= level.ProfitThreshold {
			currentSLPct = level.StopLossValue
		} else {
			break
		}
	}
	data.CurrentSLPct = currentSLPct

	// Calculate stop price based on position direction and settings
	var stopPrice float64
	if data.IsLong {
		if ts.config.SLFixed {
			stopPrice = data.EntryPrice * (1 + currentSLPct/data.Leverage/100)
		} else {
			stopPrice = data.MarkPrice - (data.MarkPrice-data.EntryPrice)*(currentSLPct/data.CurrentProfitPct)
		}
	} else {
		if ts.config.SLFixed {
			stopPrice = data.EntryPrice * (1 - currentSLPct/data.Leverage/100)
		} else {
			stopPrice = data.MarkPrice + (data.EntryPrice-data.MarkPrice)*(currentSLPct/data.CurrentProfitPct)
		}
	}

	// Calculate raw and leveraged percentages for reporting
	if data.IsLong {
		data.RawSLPct = math.Abs(((data.EntryPrice - stopPrice) / data.EntryPrice) * 100)
	} else {
		data.RawSLPct = math.Abs(((stopPrice - data.EntryPrice) / data.EntryPrice) * 100)
	}
	data.LeveragedSLPct = data.RawSLPct * data.Leverage

	return stopPrice
}

// calculateTakeProfit determines the take-profit price.
func (ts *TradingService) calculateTakeProfit(data *PositionData) float64 {
	var takePrice float64

	if data.IsLong {
		takePrice = data.EntryPrice * (1 + ts.config.TPPercent/100)
		if takePrice <= data.MarkPrice {
			takePrice = data.MarkPrice * 1.005 // Slightly above current price
		}
	} else {
		takePrice = data.EntryPrice * (1 - ts.config.TPPercent/100)
		if takePrice >= data.MarkPrice {
			takePrice = data.MarkPrice * 0.995 // Slightly below current price
		}
	}

	// Calculate raw and leveraged percentages for reporting
	if data.IsLong {
		data.RawTPPct = math.Abs(((takePrice - data.EntryPrice) / data.EntryPrice) * 100)
	} else {
		data.RawTPPct = math.Abs(((data.EntryPrice - takePrice) / data.EntryPrice) * 100)
	}
	data.LeveragedTPPct = data.RawTPPct * data.Leverage

	return takePrice
}

// getCurrentStopLoss retrieves the current stop-loss price from open orders.
func (ts *TradingService) getCurrentStopLoss(symbol string, positionSide string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Get all open orders for the symbol
	openOrders, err := ts.client.NewListOpenOrdersService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("error fetching open orders for %s: %w", symbol, err)
	}

	// Find stop-loss order
	for _, order := range openOrders {
		// Check if this is a stop-loss order (STOP_MARKET)
		if order.Type == "STOP_MARKET" {
			// Check position side based on value
			// Skip this check if positionSide is "BOTH"
			if positionSide != "BOTH" {
				// Convert both to comparable strings for safe comparison
				orderPosSide := order.PositionSide
				if (positionSide == "LONG" && orderPosSide != "LONG") ||
					(positionSide == "SHORT" && orderPosSide != "SHORT") {
					continue
				}
			}

			// Get the stop price
			stopPrice, err := strconv.ParseFloat(order.StopPrice, 64)
			if err != nil {
				return 0, fmt.Errorf("error parsing stop price: %w", err)
			}
			return stopPrice, nil
		}
	}

	// No stop-loss order found
	return 0, nil
}

// cancelExistingOrders removes all open orders for a symbol.
func (ts *TradingService) cancelExistingOrders(symbol string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	openOrders, err := ts.client.NewListOpenOrdersService().Symbol(symbol).Do(ctx)
	if err != nil {
		return fmt.Errorf("error fetching open orders for %s: %w", symbol, err)
	}

	for _, order := range openOrders {
		_, err := ts.client.NewCancelOrderService().Symbol(symbol).OrderID(order.OrderID).Do(ctx)
		if err != nil {
			log.Printf("Error canceling order %d for %s: %v", order.OrderID, symbol, err)
		}
	}
	return nil
}

// getOrderSideInfo determines the appropriate side and position side for orders.
func getOrderSideInfo(positionSide string, posAmt float64) (binance.SideType, binance.PositionSideType) {
	var closeSide binance.SideType
	var positionSideForOrder binance.PositionSideType

	if positionSide == "BOTH" {
		if posAmt > 0 {
			closeSide = binance.SideTypeSell
		} else {
			closeSide = binance.SideTypeBuy
		}
		positionSideForOrder = ""
	} else {
		positionSideForOrder = binance.PositionSideType(positionSide)
		if positionSide == "LONG" {
			closeSide = binance.SideTypeSell
		} else {
			closeSide = binance.SideTypeBuy
		}
	}

	return closeSide, positionSideForOrder
}

// createStopLossOrder places a stop-loss order for a position.
func (ts *TradingService) createStopLossOrder(data *PositionData) error {
	if data.CurrentSLPct <= 0 || data.StopPrice <= 0 || data.StopPrice == data.EntryPrice {
		return nil // No stop-loss needed
	}

	log.Printf("DEBUG SL: %s | entry: %.2f | stop: %.2f | SL%%: %.2f",
		data.Symbol, data.EntryPrice, data.StopPrice, data.CurrentSLPct)

	closeSide, positionSideForOrder := getOrderSideInfo(data.PositionSide, data.PositionAmt)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Create the stop-loss order
	slOrderService := ts.client.NewCreateOrderService().
		Symbol(data.Symbol).
		Side(closeSide).
		Type(binance.OrderTypeStopMarket).
		Quantity(data.Quantity).
		StopPrice(data.StopPriceStr).
		TimeInForce(binance.TimeInForceTypeGTC)

	if data.PositionSide != "BOTH" {
		slOrderService = slOrderService.PositionSide(positionSideForOrder)
	}

	_, err := slOrderService.Do(ctx)
	if err != nil {
		return fmt.Errorf("error setting Stop Loss order for %s: %w", data.Symbol, err)
	}
	return nil
}

// createTakeProfitOrder places a take-profit order for a position.
func (ts *TradingService) createTakeProfitOrder(data *PositionData) error {
	// Check if TP has already been reached
	if (data.IsLong && data.MarkPrice >= data.TakePrice) ||
		(data.IsShort && data.MarkPrice <= data.TakePrice) {
		log.Printf("TP for %s (%s) already reached: current price = %.2f, TP price = %.2f",
			data.Symbol, data.PositionSide, data.MarkPrice, data.TakePrice)
		return nil
	}

	closeSide, positionSideForOrder := getOrderSideInfo(data.PositionSide, data.PositionAmt)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Create the take-profit order
	tpOrderService := ts.client.NewCreateOrderService().
		Symbol(data.Symbol).
		Side(closeSide).
		Type(binance.OrderTypeTakeProfitMarket).
		Quantity(data.Quantity).
		StopPrice(data.TakePriceStr).
		TimeInForce(binance.TimeInForceTypeGTC)

	if data.PositionSide != "BOTH" {
		tpOrderService = tpOrderService.PositionSide(positionSideForOrder)
	}

	_, err := tpOrderService.Do(ctx)
	if err != nil {
		return fmt.Errorf("error setting Take Profit order for %s: %w", data.Symbol, err)
	}
	return nil
}

// formatPositionMessage creates a formatted position summary for logging and notification.
func formatPositionMessage(data *PositionData) string {
	// Determine icon based on position direction
	sideIcon := "🔴 SHORT"
	if data.IsLong {
		sideIcon = "🟢 LONG"
	}

	// Format the message
	var slText string
	if data.CurrentSLPct > 0 {
		slText = fmt.Sprintf("%.2f", data.StopPrice)
	} else {
		slText = "NONE"
	}

	var potentialLoss float64
	if data.CurrentSLPct > 0 {
		potentialLoss = math.Abs(data.PotentialLoss)
	}

	// Format the message
	msg := fmt.Sprintf(`📊 %s %s
💵 Entry: %.2f  📉 Mark: %.2f
💹 P/L: %.2f%% (%.2f%% x%d)
🛑 SL: %s (%.2f%% / %.2f%% x%d)  
🎯 TP: %.2f (%.2f%% / %.2f%% x%d)
⚖️ Risk/Reward: %.2f
💰 Potential Profit: %.2f USD
💸 Potential Loss: %.2f USD`,
		data.Symbol, sideIcon,
		data.EntryPrice, data.MarkPrice,
		data.CurrentProfitPct, data.RawProfitPct, int(data.Leverage),
		slText, data.RawSLPct, data.LeveragedSLPct, int(data.Leverage),
		data.TakePrice, data.RawTPPct, data.LeveragedTPPct, int(data.Leverage),
		data.RiskReward, data.PotentialProfit, potentialLoss)

	return msg
}

// processPosition handles a single position and manages its stop-loss and take-profit orders.
func (ts *TradingService) processPosition(position *binance.PositionRisk) error {
	// Skip empty positions
	posAmt, err := strconv.ParseFloat(position.PositionAmt, 64)
	if err != nil {
		return fmt.Errorf("error parsing position amount: %w", err)
	}

	if posAmt == 0 {
		return nil
	}

	// Extract position details
	entryPrice, err := strconv.ParseFloat(position.EntryPrice, 64)
	if err != nil {
		return fmt.Errorf("error parsing entry price: %w", err)
	}

	markPrice, err := strconv.ParseFloat(position.MarkPrice, 64)
	if err != nil {
		return fmt.Errorf("error parsing mark price: %w", err)
	}

	leverage, err := strconv.ParseFloat(position.Leverage, 64)
	if err != nil {
		return fmt.Errorf("error parsing leverage: %w", err)
	}

	symbol := position.Symbol
	positionSide := position.PositionSide

	// Get precision info for this symbol
	precision, ok := ts.symbolInfo[symbol]
	if !ok {
		return fmt.Errorf("precision information not found for %s, skipping", symbol)
	}

	// Calculate position parameters
	absAmt := math.Abs(posAmt)
	isShort := (positionSide == "SHORT" || (positionSide == "BOTH" && posAmt < 0))
	isLong := (positionSide == "LONG" || (positionSide == "BOTH" && posAmt > 0))

	// Calculate profit percentages
	var rawProfitPct float64
	if isLong {
		rawProfitPct = (markPrice - entryPrice) / entryPrice * 100
	} else if isShort {
		rawProfitPct = (entryPrice - markPrice) / entryPrice * 100
	}
	leveragedProfitPct := rawProfitPct * leverage

	// Initialize position data structure
	data := &PositionData{
		Symbol:           symbol,
		PositionSide:     positionSide,
		EntryPrice:       entryPrice,
		MarkPrice:        markPrice,
		PositionAmt:      posAmt,
		AbsAmt:           absAmt,
		Leverage:         leverage,
		IsLong:           isLong,
		IsShort:          isShort,
		CurrentProfitPct: leveragedProfitPct,
		RawProfitPct:     rawProfitPct,
	}

	// Get current stop loss from open orders
	currentSL, err := ts.getCurrentStopLoss(symbol, positionSide)
	if err != nil {
		log.Printf("Warning: Unable to get current stop loss: %v", err)
	}

	// Calculate new stop loss
	newSL := ts.calculateStopLoss(data)

	// Use the best stop loss (trailing stop logic)
	if currentSL > 0 {
		if (isLong && newSL > currentSL) || (isShort && newSL < currentSL) {
			// Update to new SL if it's more favorable
			data.StopPrice = newSL
			log.Printf("Updating SL for %s from %.2f to %.2f", symbol, currentSL, newSL)
		} else {
			// Keep existing SL
			data.StopPrice = currentSL
			log.Printf("Keeping SL for %s at %.2f (new SL %.2f is not more favorable)",
				symbol, currentSL, newSL)
		}
	} else {
		// First time setting SL
		data.StopPrice = newSL
		log.Printf("Setting first SL for %s at %.2f", symbol, newSL)
	}

	// Calculate take profit
	data.TakePrice = ts.calculateTakeProfit(data)

	// Format values according to symbol precision
	quantityFormat := fmt.Sprintf("%%.%df", precision.QuantityPrecision)
	priceFormat := fmt.Sprintf("%%.%df", precision.PricePrecision)

	data.Quantity = fmt.Sprintf(quantityFormat, absAmt)
	data.StopPriceStr = fmt.Sprintf(priceFormat, data.StopPrice)
	data.TakePriceStr = fmt.Sprintf(priceFormat, data.TakePrice)

	// Calculate potential profit and loss
	data.PotentialProfit = (data.TakePrice - data.EntryPrice) * absAmt
	if posAmt < 0 {
		data.PotentialProfit = (data.EntryPrice - data.TakePrice) * absAmt
	}

	data.PotentialLoss = 0.0
	if data.CurrentSLPct > 0 {
		data.PotentialLoss = (data.EntryPrice - data.StopPrice) * absAmt
		if posAmt < 0 {
			data.PotentialLoss = (data.StopPrice - data.EntryPrice) * absAmt
		}
	}

	// Calculate risk-reward ratio
	data.RiskReward = 0.0
	if data.PotentialLoss != 0 {
		data.RiskReward = math.Abs(data.PotentialProfit / data.PotentialLoss)
	}

	// Manage orders
	if err := ts.cancelExistingOrders(symbol); err != nil {
		log.Printf("Warning: %v", err)
	}

	if err := ts.createStopLossOrder(data); err != nil {
		log.Printf("Warning: %v", err)
	}

	if err := ts.createTakeProfitOrder(data); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Format and send position message
	msg := formatPositionMessage(data)
	fmt.Println(msg)

	if err := sendTelegramMessage(msg); err != nil {
		log.Printf("Error sending Telegram message: %v", err)
	}

	return nil
}

// processPositions processes all active positions with concurrency.
func (ts *TradingService) processPositions() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Get all positions
	positions, err := ts.client.NewGetPositionRiskService().Do(ctx)
	if err != nil {
		return fmt.Errorf("error getting positions: %w", err)
	}

	// Process positions concurrently with a wait group
	var wg sync.WaitGroup
	errChan := make(chan error, len(positions))

	for _, position := range positions {
		wg.Add(1)
		go func(pos *binance.PositionRisk) {
			defer wg.Done()
			if err := ts.processPosition(pos); err != nil {
				errChan <- fmt.Errorf("error processing position %s: %w", pos.Symbol, err)
			}
		}(position)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Collect and log errors
	for err := range errChan {
		log.Println(err)
	}

	return nil
}

// Application timeout constants.
const (
	defaultTimeout = 30 * time.Second
)

func main() {
	// Set up logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Binance Futures Guard Bot")

	// Load configuration
	config := loadConfig()

	// Setup Binance client
	client, err := setupBinanceClient()
	if err != nil {
		log.Fatalf("Error connecting to Binance API: %v", err)
	}

	// Create trading service
	tradingService, err := NewTradingService(client, config)
	if err != nil {
		log.Fatalf("Error initializing trading service: %v", err)
	}

	// Process all positions
	if err := tradingService.processPositions(); err != nil {
		log.Fatalf("Error processing positions: %v", err)
	}

	log.Println("Processing complete")
}
