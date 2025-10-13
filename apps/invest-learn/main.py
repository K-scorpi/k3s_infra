import requests
import psycopg2
from datetime import datetime
import os

# --- Настройки БД (можно вынести в .env позже) ---
DB_CONFIG = {
    "host": "localhost",
    "port": 5432,
    "database": "invest",
    "user": "invest_user",
    "password": "secure_password_123"
}

TARGET_BOARD = "TQBR"

# --- Инициализация таблицы ---
def init_db():
    conn = psycopg2.connect(**DB_CONFIG)
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS quotes (
            id SERIAL PRIMARY KEY,
            ticker TEXT NOT NULL,
            price NUMERIC(10, 4) NOT NULL,
            timestamp TIMESTAMPTZ DEFAULT NOW()
        )
    """)
    conn.commit()
    cursor.close()
    conn.close()

# --- Получение цены (без изменений) ---
def get_moex_price(ticker: str, target_board: str = TARGET_BOARD) -> float | None:
    url = f"https://iss.moex.com/iss/engines/stock/markets/shares/securities/{ticker}.json"
    response = requests.get(url)
    if response.status_code != 200:
        print(f"❌ HTTP Error: {response.status_code}")
        return None

    data = response.json()
    marketdata = data.get("marketdata", {})
    columns = marketdata.get("columns", [])
    rows = marketdata.get("data", [])

    if not columns or not rows:
        print("❌ Нет данных marketdata")
        return None

    try:
        boardid_idx = columns.index("BOARDID")
        last_idx = columns.index("LAST")
    except ValueError as e:
        print(f"❌ Колонка не найдена: {e}")
        return None

    for row in rows:
        if len(row) > boardid_idx and row[boardid_idx] == target_board:
            if len(row) > last_idx and row[last_idx] is not None:
                return float(row[last_idx])
    print(f"❌ Board {target_board} не найден или LAST = null для {ticker}")
    return None

# --- Сохранение в PostgreSQL ---
def save_price_to_db(ticker: str, price: float):
    conn = psycopg2.connect(**DB_CONFIG)
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO quotes (ticker, price) VALUES (%s, %s)",
        (ticker, price)
    )
    conn.commit()
    cursor.close()
    conn.close()
    print(f"💾 Сохранено в PostgreSQL: {ticker} = {price} ₽ в {datetime.now().isoformat()}")

# --- Основной запуск ---
if __name__ == "__main__":
    init_db()
    ticker = "SBER"
    price = get_moex_price(ticker)
    if price is not None:
        print(f"✅ Текущая цена {ticker}: {price} ₽")
        save_price_to_db(ticker, price)
    else:
        print("❌ Не удалось получить цену")