# analyzer.py
import pandas as pd
import sys
from datetime import datetime
import time
import schedule
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
        # Добавляем текущую цену в историю
        full_prices = pd.concat([hist, pd.DataFrame([{"price": row['price']}])], ignore_index=True)
        
        # Расчёт индикаторов
        full_prices['sma_5'] = SMAIndicator(close=full_prices['price'], window=5).sma_indicator()
        full_prices['sma_20'] = SMAIndicator(close=full_prices['price'], window=20).sma_indicator()
        full_prices['rsi'] = RSIIndicator(close=full_prices['price'], window=14).rsi()
        macd = MACD(close=full_prices['price'])
        full_prices['macd'] = macd.macd()
        full_prices['macd_signal'] = macd.macd_signal()
        
        last = full_prices.iloc[-1]
        reasons = []
        buy_signals = 0
        sell_signals = 0

        # BUY условия
        if last['price'] > last['sma_5']:
            reasons.append("цена выше краткосрочной средней")
            buy_signals += 1
        if last['price'] > last['sma_20']:
            reasons.append("восходящий тренд")
        if last['rsi'] < 30:
            reasons.append("акция перепродана")
            buy_signals += 1
        if not pd.isna(last['macd']) and last['macd'] > last['macd_signal']:
            reasons.append("бычий импульс")
            buy_signals += 1

        # SELL условия
        if last['rsi'] > 70:
            reasons.append("акция перекуплена")
            sell_signals += 1
        if last['price'] < last['sma_5']:
            reasons.append("цена ниже краткосрочной средней")
            sell_signals += 1
        if not pd.isna(last['macd']) and last['macd'] < last['macd_signal']:
            reasons.append("медвежий импульс")
            sell_signals += 1

        # Решение
        if buy_signals >= 2:
            signal = "BUY"
        elif sell_signals >= 2:
            signal = "SELL"
        else:
            signal = "HOLD"

        # Генерация пояснения через LLM
        meta = {
            "price": float(row['price']),
            "sma_5": float(last['sma_5']) if not pd.isna(last['sma_5']) else None,
            "sma_20": float(last['sma_20']) if not pd.isna(last['sma_20']) else None,
            "rsi": float(last['rsi']) if not pd.isna(last['rsi']) else None,
            "macd": float(last['macd']) if not pd.isna(last['macd']) else None,
            "macd_signal": float(last['macd_signal']) if not pd.isna(last['macd_signal']) else None,
            "signal": signal
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