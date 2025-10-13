import requests
import sqlite3
from datetime import datetime

# --- Настройки ---
DB_PATH = "quotes.db"
TARGET_BOARD = "TQBR"

# --- Инициализация БД ---
def init_db():
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS quotes (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            ticker TEXT NOT NULL,
            price REAL NOT NULL,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    """)
    conn.commit()
    conn.close()

# --- Получение цены с MOEX ---
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

# --- Сохранение цены в БД ---
def save_price_to_db(ticker: str, price: float):
    conn = sqlite3.connect(DB_PATH)
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO quotes (ticker, price) VALUES (?, ?)",
        (ticker, price)
    )
    conn.commit()
    conn.close()
    print(f"💾 Сохранено: {ticker} = {price} ₽ в {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")

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