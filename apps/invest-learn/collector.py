# collector.py
import time
from datetime import datetime

import requests
import schedule
from common import get_psycopg2_conn, load_tickers

TARGET_BOARD = "TQBR"
INTERVAL_MINUTES = 1  # для теста — можно 5


def get_moex_price(ticker: str) -> float | None:
    url = f"https://iss.moex.com/iss/engines/stock/markets/shares/securities/{ticker}.json"
    try:
        response = requests.get(url, timeout=10)
        if response.status_code != 200:
            return None
        data = response.json()
        marketdata = data.get("marketdata", {}).get("data", [])
        columns = data.get("marketdata", {}).get("columns", [])
        if not marketdata or not columns:
            return None
        boardid_idx = columns.index("BOARDID")
        last_idx = columns.index("LAST")
        for row in marketdata:
            if (
                row[boardid_idx] == TARGET_BOARD
                and len(row) > last_idx
                and row[last_idx] is not None
            ):
                return float(row[last_idx])
        return None
    except Exception:
        return None


def save_raw_price(ticker: str, price: float):
    conn = get_psycopg2_conn()
    cursor = conn.cursor()
    cursor.execute(
        """
        INSERT INTO quotes (ticker, price, signal) 
        VALUES (%s, %s, NULL)
    """,
        (ticker, price),
    )
    conn.commit()
    cursor.close()
    conn.close()
    print(f"📥 {ticker}: {price} ₽")


def collect():
    print(f"\n🕒 Сбор: {datetime.now().strftime('%H:%M:%S')}")
    for ticker in load_tickers():
        price = get_moex_price(ticker)
        if price is not None:
            save_raw_price(ticker, price)
        else:
            print(f"⚠️ {ticker} — нет данных")


if __name__ == "__main__":
    # Инициализация таблицы (если не создана)
    conn = get_psycopg2_conn()
    cursor = conn.cursor()
    cursor.execute(
        """
        CREATE TABLE IF NOT EXISTS quotes (
            id SERIAL PRIMARY KEY,
            ticker TEXT NOT NULL,
            price NUMERIC(10, 4) NOT NULL,
            signal TEXT,
            explanation TEXT,
            timestamp TIMESTAMPTZ DEFAULT NOW()
        );
        CREATE INDEX IF NOT EXISTS idx_ticker_timestamp ON quotes (ticker, timestamp);
    """
    )
    conn.commit()
    cursor.close()
    conn.close()

    collect()
    schedule.every(INTERVAL_MINUTES).minutes.do(collect)
    while True:
        schedule.run_pending()
        time.sleep(30)
