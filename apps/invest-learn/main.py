import requests
import psycopg2
from datetime import datetime
import schedule
import time
import os
from decimal import Decimal
import pandas as pd
from ta.momentum import RSIIndicator
from ta.trend import MACD, SMAIndicator

# --- Настройки ---
DB_CONFIG = {
    "host": "localhost",
    "port": 5432,
    "database": "invest",
    "user": "invest_user",
    "password": "secure_password_123"
}
TARGET_BOARD = "TQBR"
TICKERS_FILE = "tickers.txt"
INTERVAL_MINUTES = 1  # каждые 5 минут

# --- Инициализация БД ---
def init_db():
    conn = psycopg2.connect(**DB_CONFIG)
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS quotes (
            id SERIAL PRIMARY KEY,
            ticker TEXT NOT NULL,
            price NUMERIC(10, 4) NOT NULL,
            signal TEXT,
            timestamp TIMESTAMPTZ DEFAULT NOW()
        );
        CREATE INDEX IF NOT EXISTS idx_ticker_timestamp ON quotes (ticker, timestamp);
    """)
    conn.commit()
    cursor.close()
    conn.close()

# --- Чтение тикеров из файла ---
def load_tickers():
    if not os.path.exists(TICKERS_FILE):
        print(f"⚠️ Файл {TICKERS_FILE} не найден. Создаю пример.")
        with open(TICKERS_FILE, "w") as f:
            f.write("SBER\nGAZP\nLKOH\nYNDX\n")
        return ["SBER", "GAZP", "LKOH", "YNDX"]
    with open(TICKERS_FILE, "r") as f:
        return [line.strip().upper() for line in f if line.strip()]

# --- Получение цены (без изменений) ---
def get_moex_price(ticker: str, target_board: str = TARGET_BOARD) -> float | None:
    url = f"https://iss.moex.com/iss/engines/stock/markets/shares/securities/{ticker}.json"
    try:
        response = requests.get(url, timeout=10)
        if response.status_code != 200:
            print(f"❌ HTTP {response.status_code} для {ticker}")
            return None

        data = response.json()
        marketdata = data.get("marketdata", {})
        columns = marketdata.get("columns", [])
        rows = marketdata.get("data", [])

        if not columns or not rows:
            print(f"❌ Нет marketdata для {ticker}")
            return None

        try:
            boardid_idx = columns.index("BOARDID")
            last_idx = columns.index("LAST")
        except ValueError:
            print(f"❌ Колонки BOARDID/LAST отсутствуют для {ticker}")
            return None

        for row in rows:
            if len(row) > boardid_idx and row[boardid_idx] == target_board:
                if len(row) > last_idx and row[last_idx] is not None:
                    return float(row[last_idx])
        print(f"❌ Board {target_board} не найден или LAST=null для {ticker}")
        return None

    except Exception as e:
        print(f"❌ Ошибка при запросе {ticker}: {e}")
        return None

# --- Сохранение в БД ---
def save_price_with_signal(ticker: str, price: float, signal: str):
    try:
        conn = psycopg2.connect(**DB_CONFIG)
        cursor = conn.cursor()
        cursor.execute(
            "INSERT INTO quotes (ticker, price, signal) VALUES (%s, %s, %s)",
            (ticker, price, signal)
        )
        conn.commit()
        cursor.close()
        conn.close()
        print(f"- {ticker}: {price} ₽ | {signal}")
    except Exception as e:
        print(f"❌ Ошибка записи в БД для {ticker}: {e}")

# --- Основная задача сбора ---
def fetch_all_tickers():
    print(f"\n🕒 Сбор котировок: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    tickers = load_tickers()
    for ticker in tickers:
        price = get_moex_price(ticker)
        if price is not None:
            signal = calculate_signal(ticker, price, window=5)
            save_price_with_signal(ticker, price, signal)
        else:
            print(f"⚠️ Пропущен {ticker}")

def calculate_signal(ticker: str, current_price: float, window: int = 5) -> str:
    """
    Возвращает: 'BUY', 'SELL', 'NO_SIGNAL'
    """
    try:
        conn = psycopg2.connect(**DB_CONFIG)
        cursor = conn.cursor()
        cursor.execute("""
            SELECT price FROM quotes
            WHERE ticker = %s
            ORDER BY timestamp DESC
            LIMIT %s
        """, (ticker, window - 1))

        # Преобразуем Decimal → float
        past_prices = [float(row[0]) for row in cursor.fetchall()]
        cursor.close()
        conn.close()

        # Добавляем текущую цену (уже float)
        all_prices = past_prices[::-1] + [current_price]  # хронологический порядок

        if len(all_prices) < window:
            return "NO_SIGNAL"

        sma = sum(all_prices[-window:]) / window
        if current_price > sma:
            return "BUY"
        elif current_price < sma:
            return "SELL"
        else:
            return "HOLD"

    except Exception as e:
        print(f"⚠️ Ошибка расчёта сигнала для {ticker}: {e}")
        return "NO_SIGNAL"  
# --- Главный цикл ---
if __name__ == "__main__":
    init_db()
    tickers = load_tickers()
    print(f"📈 Загружено тикеров: {len(tickers)} — {', '.join(tickers)}")

    # Первый запуск сразу
    fetch_all_tickers()

    # Планировщик
    schedule.every(INTERVAL_MINUTES).minutes.do(fetch_all_tickers)

    print(f"⏱️  Следующий сбор через {INTERVAL_MINUTES} минут...")
    while True:
        schedule.run_pending()
        time.sleep(30)  # проверка каждые 30 сек