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
		{300, 0},     // Initial stage, no SL adjustment yet
		{450, 150},   // Start light capital protection
		{600, 300},   // RR 1:1, begin locking in profits
		{750, 450},   // Move SL higher but still leave room for breakout
		{900, 600},   // RR 1.5:1, locking more profit
		{1050, 750},  // Gradually increase the protection level
		{1200, 900},  // Secure at least 900 in profit
		{1350, 1050}, // Protect 1050 profit level
		{1500, 1200}, // Lock in a solid 1200 profit
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

// Fixed calculateStopLoss function with precise calculations
func (ts *TradingService) calculateStopLoss(data *PositionData) float64 {
	// Determine current stop-loss percentage based on profit levels
	currentSLPct := ts.config.DefaultSLPercent
	log.Printf("DEBUG: Initial SL%% for %s set to %.2f%%", data.Symbol, currentSLPct)

	// Only adjust stop-loss based on profit levels if we're in profit
	// and hit at least the first threshold
	if data.CurrentProfitPct > 0 {
		thresholdReached := false
		for _, level := range ts.stopLevels {
			if data.CurrentProfitPct >= level.ProfitThreshold {
				currentSLPct = level.StopLossValue
				thresholdReached = true
				log.Printf("DEBUG: Adjusted SL%% to %.2f%% based on profit threshold %.2f%%",
					currentSLPct, level.ProfitThreshold)
			} else {
				break
			}
		}

		// If no threshold reached but in profit, keep using the default SL percent
		if !thresholdReached {
			log.Printf("DEBUG: Using default SL%% of %.2f%% for position with profit %.2f%% (below first threshold)",
				currentSLPct, data.CurrentProfitPct)
		}
	}
	data.CurrentSLPct = currentSLPct

	// Calculate stop price based on position direction
	var stopPrice float64

	// IMPORTANT FIX: For positions below threshold, the SL calculation logic was incorrect
	// For default behavior (below thresholds), we want:
	// - Long positions: SL below entry price by DefaultSLPercent
	// - Short positions: SL above entry price by DefaultSLPercent

	if data.IsLong {
		if currentSLPct == 0 {
			// At breakeven
			stopPrice = data.EntryPrice
			log.Printf("DEBUG: Long SL calculation: Breakeven at entry=%.8f", data.EntryPrice)
		} else if data.CurrentProfitPct >= ts.stopLevels[0].ProfitThreshold {
			// We're above threshold - lock in profit at specified level above entry
			profitPercentToSecure := currentSLPct / data.Leverage
			stopPrice = data.EntryPrice * (1 + profitPercentToSecure/100)
			log.Printf("DEBUG: Long SL calculation (above threshold): Entry=%.8f * (1 + %.4f/100) = %.8f",
				data.EntryPrice, profitPercentToSecure, stopPrice)
		} else {
			// Default behavior - SL below entry by DefaultSLPercent
			// Note: This is a fixed percentage of the entry price
			rawSLPct := currentSLPct
			stopPrice = data.EntryPrice * (1 - rawSLPct/100)
			log.Printf("DEBUG: Long SL calculation (below threshold): Entry=%.8f * (1 - %.4f/100) = %.8f",
				data.EntryPrice, rawSLPct, stopPrice)
		}
	} else {
		// For short positions
		if currentSLPct == 0 {
			// At breakeven
			stopPrice = data.EntryPrice
			log.Printf("DEBUG: Short SL calculation: Breakeven at entry=%.8f", data.EntryPrice)
		} else if data.CurrentProfitPct >= ts.stopLevels[0].ProfitThreshold {
			// We're above threshold - lock in profit at specified level below entry
			profitPercentToSecure := currentSLPct / data.Leverage
			stopPrice = data.EntryPrice * (1 - profitPercentToSecure/100)
			log.Printf("DEBUG: Short SL calculation (above threshold): Entry=%.8f * (1 - %.4f/100) = %.8f",
				data.EntryPrice, profitPercentToSecure, stopPrice)
		} else {
			// Default behavior - SL above entry by DefaultSLPercent
			// THIS IS THE KEY FIX - for positions below threshold, we use the raw percentage
			// directly (not divided by leverage) to calculate the stop price
			rawSLPct := currentSLPct
			stopPrice = data.EntryPrice * (1 + rawSLPct/100)
			log.Printf("DEBUG: Short SL calculation (below threshold): Entry=%.8f * (1 + %.4f/100) = %.8f",
				data.EntryPrice, rawSLPct, stopPrice)
		}
	}

	// Calculate raw and leveraged percentages for reporting
	if data.IsLong {
		if stopPrice >= data.EntryPrice {
			// SL is above entry (in profit)
			data.RawSLPct = ((stopPrice - data.EntryPrice) / data.EntryPrice) * 100
		} else {
			// SL is below entry (at loss)
			data.RawSLPct = -((data.EntryPrice - stopPrice) / data.EntryPrice) * 100
		}
	} else {
		if stopPrice <= data.EntryPrice {
			// SL is below entry (in profit)
			data.RawSLPct = ((data.EntryPrice - stopPrice) / data.EntryPrice) * 100
		} else {
			// SL is above entry (at loss)
			data.RawSLPct = -((stopPrice - data.EntryPrice) / data.EntryPrice) * 100
		}
	}
	data.LeveragedSLPct = data.RawSLPct * data.Leverage

	log.Printf("DEBUG: Final SL for %s: price=%.8f, raw=%.2f%%, leveraged=%.2f%%",
		data.Symbol, stopPrice, data.RawSLPct, data.LeveragedSLPct)

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
	if data.CurrentSLPct < 0 || data.StopPrice <= 0 {
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
	if data.CurrentSLPct >= 0 && data.StopPrice > 0 {
		slText = fmt.Sprintf("%.8f", data.StopPrice)
	} else {
		slText = "NONE"
	}
	var potentialLossDisplay float64
	if data.CurrentSLPct > 0 {
		potentialLossDisplay = math.Abs(data.PotentialLoss)
	} else {
		potentialLossDisplay = math.Abs(data.PotentialLoss)
	}

	// Add negative sign to potential loss when RawSLPct is negative
	if data.RawSLPct < 0 {
		potentialLossDisplay = -potentialLossDisplay
	}

	// Format the message with higher precision for price values
	msg := fmt.Sprintf(`📊 %s %s
💵 Entry: %.8f  📉 Mark: %.8f
💹 P/L: %.2f%% (%.2f%% x%d)
🛑 SL: %s (%.2f%% / %.2f%% x%d)  
🎯 TP: %.8f (%.2f%% / %.2f%% x%d)
⚖️ Risk/Reward: %.2f
💰 Potential Profit: %.2f USD
💸 Potential Loss: %.2f USD`,
		data.Symbol, sideIcon,
		data.EntryPrice, data.MarkPrice,
		data.CurrentProfitPct, data.RawProfitPct, int(data.Leverage),
		slText, data.RawSLPct, data.LeveragedSLPct, int(data.Leverage),
		data.TakePrice, data.RawTPPct, data.LeveragedTPPct, int(data.Leverage),
		data.RiskReward, data.PotentialProfit, potentialLossDisplay)
	return msg
}

// getCurrentTakeProfit retrieves the current take-profit price from open orders.
func (ts *TradingService) getCurrentTakeProfit(symbol string, positionSide string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Get all open orders for the symbol
	openOrders, err := ts.client.NewListOpenOrdersService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("error fetching open orders for %s: %w", symbol, err)
	}

	// Find take-profit order
	for _, order := range openOrders {
		// Check if this is a take-profit order (TAKE_PROFIT_MARKET)
		if order.Type == "TAKE_PROFIT_MARKET" {
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

			// Get the stop price (which is actually the take-profit price in this case)
			takePrice, err := strconv.ParseFloat(order.StopPrice, 64)
			if err != nil {
				return 0, fmt.Errorf("error parsing take profit price: %w", err)
			}
			return takePrice, nil
		}
	}

	// No take-profit order found
	return 0, nil
}

// updatePositionOrders cancels existing orders and creates new ones only if necessary
func (ts *TradingService) updatePositionOrders(data *PositionData) error {
	// Get current stop loss and take profit from open orders
	currentSL, err := ts.getCurrentStopLoss(data.Symbol, data.PositionSide)
	if err != nil {
		log.Printf("Warning: Unable to get current stop loss: %v", err)
	}

	currentTP, err := ts.getCurrentTakeProfit(data.Symbol, data.PositionSide)
	if err != nil {
		log.Printf("Warning: Unable to get current take profit: %v", err)
	}

	// Calculate new stop loss
	newSL := ts.calculateStopLoss(data)
	// Store the newly calculated RawSLPct
	newRawSLPct := data.RawSLPct

	// Determine which profit threshold we're at
	currentThreshold := -1
	for i, level := range ts.stopLevels {
		if data.CurrentProfitPct >= level.ProfitThreshold {
			currentThreshold = i
		} else {
			break
		}
	}

	// Determine if we need to update the stop loss
	slNeedsUpdate := true
	if currentSL > 0 {
		// Calculate raw percentage of current SL
		var currentRawSLPct float64
		if data.IsLong {
			if currentSL >= data.EntryPrice {
				currentRawSLPct = ((currentSL - data.EntryPrice) / data.EntryPrice) * 100
			} else {
				currentRawSLPct = -((data.EntryPrice - currentSL) / data.EntryPrice) * 100
			}
		} else {
			if currentSL <= data.EntryPrice {
				currentRawSLPct = ((data.EntryPrice - currentSL) / data.EntryPrice) * 100
			} else {
				currentRawSLPct = -((currentSL - data.EntryPrice) / data.EntryPrice) * 100
			}
		}
		currentLeveragedSLPct := currentRawSLPct * data.Leverage

		// Calculate which threshold the current SL corresponds to
		currentSLThreshold := -1
		for i, level := range ts.stopLevels {
			if math.Abs(currentRawSLPct-level.StopLossValue/data.Leverage) < 0.1 {
				currentSLThreshold = i
				break
			}
		}

		priceDifference := math.Abs(currentSL - newSL)
		slPriceThreshold := 0.0001 * data.EntryPrice

		log.Printf("DEBUG: Current SL threshold: %d, New threshold: %d, Current profit: %.2f%%",
			currentSLThreshold, currentThreshold, data.CurrentProfitPct)

		// FORCE UPDATE SL IF WE'VE CROSSED A NEW THRESHOLD
		if currentThreshold > currentSLThreshold {
			// We've crossed a new threshold, definitely update
			data.StopPrice = newSL
			log.Printf("THRESHOLD CROSSED: Updating SL for %s from %.4f to %.4f (threshold %d -> %d)",
				data.Symbol, currentSL, newSL, currentSLThreshold, currentThreshold)
		} else if currentRawSLPct > newRawSLPct || priceDifference < slPriceThreshold {
			// If current SL price is better or difference is too small, do not update
			data.StopPrice = currentSL
			// Update percentage values to ensure consistency
			data.RawSLPct = currentRawSLPct
			data.LeveragedSLPct = currentLeveragedSLPct
			slNeedsUpdate = false
			log.Printf("Keeping SL for %s at %.4f (raw %.4f%% > new %.4f%% or diff %.6f < threshold %.6f)",
				data.Symbol, currentSL, currentRawSLPct, newRawSLPct, priceDifference, slPriceThreshold)
		} else {
			// New SL percentage is greater or equal
			data.StopPrice = newSL
			log.Printf("Updating SL for %s from %.4f to %.4f (raw %.4f%% to %.4f%%, diff %.6f)",
				data.Symbol, currentSL, newSL, currentRawSLPct, newRawSLPct, priceDifference)
		}
	} else {
		// First time setting SL
		data.StopPrice = newSL
		log.Printf("Setting first SL for %s at %.4f (%.4f%% raw)",
			data.Symbol, newSL, newRawSLPct)
	}

	// Calculate take profit
	newTP := ts.calculateTakeProfit(data)
	data.TakePrice = newTP

	// Check if TP has already been reached
	tpReached := (data.IsLong && data.MarkPrice >= data.TakePrice) ||
		(data.IsShort && data.MarkPrice <= data.TakePrice)

	// Debug logs for TP values
	log.Printf("TP Debug for %s: Current TP = %.4f, New calculated TP = %.4f, Mark price = %.4f",
		data.Symbol, currentTP, newTP, data.MarkPrice)

	// Determine if we need to update the take profit
	tpNeedsUpdate := false // Default to NOT updating

	// First check if we don't have a TP yet
	if currentTP <= 0 {
		// No current TP exists, we need to create one
		tpNeedsUpdate = true
		log.Printf("No existing TP for %s, will create new TP at %.4f",
			data.Symbol, newTP)
	} else {
		// Calculate the difference between current and new TP as a percentage
		tpDiffPercent := math.Abs((currentTP - newTP) / currentTP * 100)
		log.Printf("TP difference for %s: %.4f%% (current: %.4f, new: %.4f)",
			data.Symbol, tpDiffPercent, currentTP, newTP)

		// Only update if the difference is significant (e.g., more than 0.5%)
		if tpDiffPercent > 0.5 {
			tpNeedsUpdate = true
			log.Printf("TP difference %.4f%% is significant, will update TP for %s from %.4f to %.4f",
				tpDiffPercent, data.Symbol, currentTP, newTP)
		} else {
			// Keep the current TP if difference is small
			data.TakePrice = currentTP
			log.Printf("Keeping current TP for %s at %.4f (difference %.4f%% is insignificant)",
				data.Symbol, currentTP, tpDiffPercent)
		}
	}

	// Check if TP has already been reached (price touched/crossed TP level)
	if tpReached {
		log.Printf("TP for %s (%s) already reached: current price = %.4f, TP price = %.4f",
			data.Symbol, data.PositionSide, data.MarkPrice, data.TakePrice)
		// If TP is reached but we have no TP order, we should still set one slightly above/below current price
		if currentTP <= 0 {
			tpNeedsUpdate = true
			log.Printf("TP already reached but no TP order exists, will create one for %s", data.Symbol)
		}
	}

	// Format values according to symbol precision
	precision, ok := ts.symbolInfo[data.Symbol]
	if !ok {
		return fmt.Errorf("precision information not found for %s", data.Symbol)
	}

	quantityFormat := fmt.Sprintf("%%.%df", precision.QuantityPrecision)
	priceFormat := fmt.Sprintf("%%.%df", precision.PricePrecision)

	data.Quantity = fmt.Sprintf(quantityFormat, data.AbsAmt)
	data.StopPriceStr = fmt.Sprintf(priceFormat, data.StopPrice)
	data.TakePriceStr = fmt.Sprintf(priceFormat, data.TakePrice)

	// Calculate potential profit and loss
	data.PotentialProfit = (data.TakePrice - data.EntryPrice) * data.AbsAmt
	if data.PositionAmt < 0 {
		data.PotentialProfit = (data.EntryPrice - data.TakePrice) * data.AbsAmt
	}

	// FIXED: Calculate potential loss correctly based on stop price, regardless of CurrentSLPct
	data.PotentialLoss = 0.0
	if data.StopPrice > 0 {
		if data.IsLong {
			// For long positions, loss is when price goes below entry
			data.PotentialLoss = (data.StopPrice - data.EntryPrice) * data.AbsAmt
		} else {
			// For short positions, loss is when price goes above entry
			data.PotentialLoss = (data.EntryPrice - data.StopPrice) * data.AbsAmt
		}
		// If the calculation results in a positive value for what should be a loss, negate it
		if data.PotentialLoss > 0 {
			data.PotentialLoss = -data.PotentialLoss
		}
	}

	// Calculate risk-reward ratio
	data.RiskReward = 0.0
	if data.PotentialLoss != 0 {
		data.RiskReward = math.Abs(data.PotentialProfit / data.PotentialLoss)
	}

	log.Printf("Order update status for %s: SL needs update: %v, TP needs update: %v",
		data.Symbol, slNeedsUpdate, tpNeedsUpdate)

	// We'll handle SL and TP separately to avoid unnecessary cancellations
	if slNeedsUpdate && tpNeedsUpdate {
		// Both need updates, cancel all and recreate both
		log.Printf("Both SL and TP need updates for %s, cancelling all orders", data.Symbol)
		if err := ts.cancelExistingOrders(data.Symbol); err != nil {
			log.Printf("Warning: %v", err)
		}

		// Create new SL order
		if err := ts.createStopLossOrder(data); err != nil {
			log.Printf("Warning: %v", err)
		} else {
			log.Printf("Successfully created new SL order for %s at %s", data.Symbol, data.StopPriceStr)
		}

		// Create new TP order
		if err := ts.createTakeProfitOrder(data); err != nil {
			log.Printf("Warning: %v", err)
		} else {
			log.Printf("Successfully created new TP order for %s at %s", data.Symbol, data.TakePriceStr)
		}
	} else if slNeedsUpdate {
		// Only SL needs update
		log.Printf("Only SL needs update for %s", data.Symbol)

		// Get all open orders to find and cancel only SL
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		openOrders, err := ts.client.NewListOpenOrdersService().Symbol(data.Symbol).Do(ctx)
		if err != nil {
			log.Printf("Error fetching open orders for selective cancellation: %v", err)
			return err
		}

		// Find and cancel only stop-loss orders
		for _, order := range openOrders {
			if order.Type == "STOP_MARKET" {
				_, err := ts.client.NewCancelOrderService().Symbol(data.Symbol).OrderID(order.OrderID).Do(ctx)
				if err != nil {
					log.Printf("Error canceling SL order %d for %s: %v", order.OrderID, data.Symbol, err)
				} else {
					log.Printf("Successfully cancelled SL order %d for %s", order.OrderID, data.Symbol)
				}
			}
		}

		// Create new SL order
		if err := ts.createStopLossOrder(data); err != nil {
			log.Printf("Warning: %v", err)
		} else {
			log.Printf("Successfully created new SL order for %s at %s", data.Symbol, data.StopPriceStr)
		}
	} else if tpNeedsUpdate {
		// Only TP needs update
		log.Printf("Only TP needs update for %s", data.Symbol)

		// Get all open orders to find and cancel only TP
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()

		openOrders, err := ts.client.NewListOpenOrdersService().Symbol(data.Symbol).Do(ctx)
		if err != nil {
			log.Printf("Error fetching open orders for selective cancellation: %v", err)
			return err
		}

		// Find and cancel only take-profit orders
		for _, order := range openOrders {
			if order.Type == "TAKE_PROFIT_MARKET" {
				_, err := ts.client.NewCancelOrderService().Symbol(data.Symbol).OrderID(order.OrderID).Do(ctx)
				if err != nil {
					log.Printf("Error canceling TP order %d for %s: %v", order.OrderID, data.Symbol, err)
				} else {
					log.Printf("Successfully cancelled TP order %d for %s", order.OrderID, data.Symbol)
				}
			}
		}

		// Create new TP order
		if err := ts.createTakeProfitOrder(data); err != nil {
			log.Printf("Warning: %v", err)
		} else {
			log.Printf("Successfully created new TP order for %s at %s", data.Symbol, data.TakePriceStr)
		}
	} else {
		log.Printf("No changes needed for %s orders", data.Symbol)
	}

	return nil
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

	// Check if we have precision info for this symbol
	if _, ok := ts.symbolInfo[symbol]; !ok {
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

	// Update orders (this will handle SL and TP checking and placement)
	if err := ts.updatePositionOrders(data); err != nil {
		return fmt.Errorf("error updating orders: %w", err)
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
