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
from sqlalchemy import create_engine
from llm import explain_signal_with_llm

# --- Настройки ---
DB_CONFIG = {
    "host": "localhost",
    "port": 5432,
    "database": "invest",
    "user": "invest_user",
    "password": "secure_password_123"
}

engine = create_engine(f"postgresql://{DB_CONFIG['user']}:{DB_CONFIG['password']}@{DB_CONFIG['host']}:{DB_CONFIG['port']}/{DB_CONFIG['database']}")

TARGET_BOARD = "TQBR"
TICKERS_FILE = "tickers.txt"
INTERVAL_MINUTES = 5  # каждые 5 минут

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
    print(f"\n🕒 Сбор и анализ: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    tickers = load_tickers()
    for ticker in tickers:
        price = get_moex_price(ticker)
        if price is not None:
            signal, meta = calculate_advanced_signal(ticker, price)
            explanation = explain_signal_with_llm(ticker, meta)
            save_price_with_signal(ticker, price, signal)
            print(f"📊 {ticker}: {price} ₽ | {signal}")
            print(f"💬 {explanation}\n")
        else:
            print(f"⚠️ Пропущен {ticker}")

def calculate_advanced_signal(ticker: str, current_price: float, current_volume: int = 0) -> tuple[str, dict]:
    """
    Возвращает: (сигнал, метаданные для LLM)
    """
    try:
        conn = psycopg2.connect(**DB_CONFIG)
        # Получаем последние 50 записей для надёжного расчёта
        df = pd.read_sql("""
        SELECT price, timestamp 
        FROM quotes 
        WHERE ticker = %s 
        ORDER BY timestamp ASC
        """, engine, params=(ticker,))
        conn.close()

        if df.empty:
            return "NO_DATA", {}

        # Добавляем текущую цену как новую строку (пока без timestamp)
        new_row = pd.DataFrame([{"price": current_price, "timestamp": datetime.utcnow()}])
        df = pd.concat([df, new_row], ignore_index=True)

        # Убедимся, что достаточно данных
        if len(df) < 20:
            return "INSUFFICIENT_DATA", {}

        # --- Индикаторы ---
        df['sma_5'] = SMAIndicator(close=df['price'], window=5).sma_indicator()
        df['sma_20'] = SMAIndicator(close=df['price'], window=20).sma_indicator()
        df['rsi'] = RSIIndicator(close=df['price'], window=14).rsi()
        macd = MACD(close=df['price'])
        df['macd'] = macd.macd()
        df['macd_signal'] = macd.macd_signal()

        # Берём последнюю строку
        last = df.iloc[-1]

        signals = []
        reasons = []

        # 1. SMA 5 vs цена
        if last['price'] > last['sma_5']:
            signals.append("SMA5_BULL")
            reasons.append("цена выше 5-минутной скользящей средней")

        # 2. SMA 20 (тренд)
        if last['price'] > last['sma_20']:
            signals.append("TREND_UP")
            reasons.append("восходящий тренд (цена выше SMA20)")

        # 3. RSI
        if last['rsi'] < 30:
            signals.append("RSI_OVERSOLD")
            reasons.append("акция перепродана (RSI < 30)")
        elif last['rsi'] > 70:
            signals.append("RSI_OVERBOUGHT")
            reasons.append("акция перекуплена (RSI > 70)")

        # 4. MACD
        if not pd.isna(last['macd']) and not pd.isna(last['macd_signal']):
            if last['macd'] > last['macd_signal']:
                signals.append("MACD_BULL")
                reasons.append("бычий сигнал MACD")

        # Решение
        buy_signals = len([s for s in signals if "BULL" in s or "OVERSOLD" in s])
        sell_signals = len([s for s in signals if "OVERBOUGHT" in s])

        if buy_signals >= 2:
            signal = "BUY"
        elif sell_signals >= 1 and "TREND_UP" not in signals:
            signal = "SELL"
        else:
            signal = "HOLD"

        metadata = {
            "price": float(current_price),
            "sma_5": float(last['sma_5']) if not pd.isna(last['sma_5']) else None,
            "sma_20": float(last['sma_20']) if not pd.isna(last['sma_20']) else None,
            "rsi": float(last['rsi']) if not pd.isna(last['rsi']) else None,
            "macd": float(last['macd']) if not pd.isna(last['macd']) else None,
            "macd_signal": float(last['macd_signal']) if not pd.isna(last['macd_signal']) else None,
            "reasons": reasons,
            "signal": signal
        }

        return signal, metadata

    except Exception as e:
        print(f"⚠️ Ошибка продвинутого анализа для {ticker}: {e}")
        return "ERROR", {}
    
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