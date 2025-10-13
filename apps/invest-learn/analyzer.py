# analyzer.py
import pandas as pd
import sys
from datetime import datetime
import time
import schedule
from ta.volatility import BollingerBands, AverageTrueRange
from ta.volume import VolumeWeightedAveragePrice
from ta.momentum import RSIIndicator
from ta.trend import MACD, SMAIndicator
from sqlalchemy import text
from common import get_sqlalchemy_engine, load_tickers
from llm import explain_signal_with_llm

engine = get_sqlalchemy_engine()

def analyze_ticker(ticker: str):
    # Получаем записи без сигнала
    df = pd.read_sql("""
        SELECT id, price FROM quotes 
        WHERE ticker = %s AND signal IS NULL 
        ORDER BY timestamp ASC
        LIMIT 1
    """, engine, params=(ticker,))
    if df.empty:
        return

    # Получаем полную историю цен для расчёта индикаторов
    hist = pd.read_sql("""
        SELECT price FROM quotes 
        WHERE ticker = %s 
        ORDER BY timestamp ASC
    """, engine, params=(ticker,))
    
    if len(hist) < 20:
        print(f"⏳ {ticker}: недостаточно данных для анализа")
        return

    for _, row in df.iterrows():
        full_prices = pd.concat([hist, pd.DataFrame([{"price": row['price']}])], ignore_index=True)
        
        # --- Основные индикаторы ---
        full_prices['sma_5'] = SMAIndicator(close=full_prices['price'], window=5).sma_indicator()
        full_prices['sma_20'] = SMAIndicator(close=full_prices['price'], window=20).sma_indicator()
        full_prices['rsi'] = RSIIndicator(close=full_prices['price'], window=14).rsi()
        macd_obj = MACD(close=full_prices['price'])
        full_prices['macd'] = macd_obj.macd()
        full_prices['macd_signal'] = macd_obj.macd_signal()

        # --- Дополнительные индикаторы ---
        bb = BollingerBands(close=full_prices['price'], window=20, window_dev=2)
        full_prices['bb_high'] = bb.bollinger_hband()
        full_prices['bb_low'] = bb.bollinger_lband()
        full_prices['bb_mid'] = bb.bollinger_mavg()
        full_prices['atr'] = AverageTrueRange(high=full_prices['price'], low=full_prices['price'], close=full_prices['price'], window=14).average_true_range()

        last = full_prices.iloc[-1]
        prev = full_prices.iloc[-2] if len(full_prices) > 1 else None

        reasons = []
        buy_signals = 0
        sell_signals = 0

        # --- Цена и скользящие ---
        if last['price'] > last['sma_20']:
            reasons.append("цена выше 20-дневной SMA — восходящий тренд")
            buy_signals += 1
        if last['price'] < last['sma_20']:
            reasons.append("цена ниже 20-дневной SMA — нисходящий тренд")
            sell_signals += 1

        # --- RSI ---
        if last['rsi'] < 30:
            reasons.append("RSI < 30 — акция перепродана")
            buy_signals += 1
        elif last['rsi'] > 70:
            reasons.append("RSI > 70 — акция перекуплена")
            sell_signals += 1

        # --- Дивергенс (упрощённо) ---
        if prev is not None and last['rsi'] < prev['rsi'] and last['price'] > prev['price']:
            reasons.append("медвежий дивергенс по RSI — возможен разворот вниз")
            sell_signals += 1
        elif prev is not None and last['rsi'] > prev['rsi'] and last['price'] < prev['price']:
            reasons.append("бычий дивергенс по RSI — возможен разворот вверх")
            buy_signals += 1

        # --- Bollinger Bands ---
        if last['price'] > last['bb_high']:
            reasons.append("цена выше верхней полосы Боллинджера — перекупленность")
            sell_signals += 1
        elif last['price'] < last['bb_low']:
            reasons.append("цена ниже нижней полосы Боллинджера — перепроданность")
            buy_signals += 1

        # --- MACD ---
        if not pd.isna(last['macd']) and not pd.isna(last['macd_signal']):
            if last['macd'] > last['macd_signal']:
                reasons.append("MACD выше сигнальной линии — бычий импульс")
                buy_signals += 1
            else:
                reasons.append("MACD ниже сигнальной линии — медвежий импульс")
                sell_signals += 1

        # --- Решение ---
        if buy_signals >= 2 and sell_signals == 0:
            signal = "BUY"
        elif sell_signals >= 2 and buy_signals == 0:
            signal = "SELL"
        else:
            signal = "HOLD"

        # --- Метаданные для LLM ---
        meta = {
            "price": float(row['price']),
            "sma_5": float(last['sma_5']) if not pd.isna(last['sma_5']) else None,
            "sma_20": float(last['sma_20']) if not pd.isna(last['sma_20']) else None,
            "rsi": float(last['rsi']) if not pd.isna(last['rsi']) else None,
            "macd": float(last['macd']) if not pd.isna(last['macd']) else None,
            "macd_signal": float(last['macd_signal']) if not pd.isna(last['macd_signal']) else None,
            "bb_high": float(last['bb_high']) if not pd.isna(last['bb_high']) else None,
            "bb_low": float(last['bb_low']) if not pd.isna(last['bb_low']) else None,
            "atr": float(last['atr']) if not pd.isna(last['atr']) else None,
            "signal": signal,
            "reasons": reasons
        }
        
        if signal == "HOLD" and len(reasons) <= 1:
            explanation = "Недостаточно данных для анализа."
        else:
            explanation = explain_signal_with_llm(ticker, meta)

        # Обновление записи в БД (с приведением типов!)
        with engine.connect() as conn:
            conn.execute(
                text("""
                    UPDATE quotes 
                    SET signal = :signal, explanation = :explanation 
                    WHERE id = :id
                """),
                {
                    "signal": signal,
                    "explanation": explanation,
                    "id": int(row['id'])  # ← ключевое: numpy → int
                }
            )
            conn.commit()

        print(f"🧠 {ticker}: {signal}")
        print(f"💬 {explanation}\n")

def analyze_all():
    print(f"\n🔍 Анализ: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    for ticker in load_tickers():
        try:
            analyze_ticker(ticker)
        except Exception as e:
            print(f"⚠️ Ошибка при анализе {ticker}: {e}")

# --- Запуск ---
if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--once":
        analyze_all()
    else:
        # Фоновый режим: анализ каждые 2 минуты
        analyze_all()
        schedule.every(2).minutes.do(analyze_all)
        while True:
            schedule.run_pending()
            time.sleep(30)