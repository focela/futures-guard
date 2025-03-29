# Futures Guard

A robust, automated position manager for Binance Futures trading with configurable stop-loss and take-profit orders.

## Features

- **Automated Position Management**: Monitors and manages your open Binance futures positions
- **Dynamic Stop-Loss Levels**: Adjusts stop-loss based on profit thresholds
- **Take-Profit Automation**: Sets take-profit orders at configurable levels
- **Risk Management**: Calculates risk/reward ratios for each position
- **Real-time Notifications**: Sends detailed position updates via Telegram
- **Containerized Deployment**: Ready-to-use Docker configuration

## Requirements

- Go 1.24+
- Binance Futures account with API access
- Telegram bot for notifications (optional but recommended)

## Installation

### Local Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/focela/futures-guard.git
   cd futures-guard
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the application:
   ```bash
   go build -o futures-guard main.go
   ```

4. Configure your environment (see Configuration section)

5. Run the application:
   ```bash
   ./futures-guard
   ```

### Docker Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/focela/futures-guard.git
   cd futures-guard
   ```

2. Create your `.env` file based on `.env.example`

3. Start with Docker Compose:
   ```bash
   docker-compose up -d
   ```

## Configuration

Create a `.env` file in the project root based on the provided `.env.example`:

```
# Binance API credentials
# Required for accessing the Binance Futures API
BINANCE_API_KEY=your_binance_api_key_here
BINANCE_API_SECRET=your_binance_api_secret_here

# Telegram notification settings
# Required for sending position updates and alerts
TELEGRAM_BOT_TOKEN=your_telegram_bot_token_here
TELEGRAM_CHAT_ID=your_telegram_chat_id_here

# Trading configuration
# Controls the risk management behavior of the bot
# Default stop-loss percentage if no other conditions are met (e.g., 1.0)
DEFAULT_SL_PERCENT=1.0
# Fixed take-profit percentage (e.g., 3.0)
TP_PERCENT=3.0
# When true, uses stop-loss based on fixed profit calculation
# When false, uses stop-loss based on current market price
SL_FIXED=true
```

### Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `BINANCE_API_KEY` | Your Binance API key | (Required) |
| `BINANCE_API_SECRET` | Your Binance API secret | (Required) |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token for notifications | (Optional) |
| `TELEGRAM_CHAT_ID` | Telegram chat ID to receive notifications | (Optional) |
| `DEFAULT_SL_PERCENT` | Default stop-loss percentage | 1.0 |
| `TP_PERCENT` | Take-profit percentage | 3.0 |
| `SL_FIXED` | Use fixed SL calculation when true | true |

## Usage

### Running Manually

Run the application directly:

```bash
./futures-guard
```

### Setting Up as a Service

For production use, it's recommended to set up the application as a system service or using a process manager like `systemd` or `supervisor`.

#### Using Cron

You can also run the application as a scheduled task using cron:

```
# Run every minute
* * * * * cd /path/to/futures-guard && ./futures-guard > /path/to/logfile.log 2>&1
```

### Using Docker

Start the containerized application:

```bash
docker-compose up -d
```

View logs:

```bash
docker-compose logs -f
```

Stop the application:

```bash
docker-compose down
```

## How It Works

1. The bot connects to Binance Futures API using your credentials
2. It fetches all your open positions
3. For each position, it:
    - Calculates current profit/loss
    - Determines appropriate stop-loss level based on profit thresholds
    - Sets take-profit orders according to configuration
    - Sends position details via Telegram
4. The process repeats when you run the bot again (recommended to run periodically via cron or as a service)

### Stop-Loss Calculation

The bot uses a tiered approach to stop-loss:
- For positions with lower profits, it maintains tighter stop-loss
- As profit increases, stop-loss levels are moved to lock in more profit
- The exact calculation depends on whether `SL_FIXED` is true or false

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
