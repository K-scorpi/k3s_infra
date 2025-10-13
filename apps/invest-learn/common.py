# common.py
import os

import psycopg2
from dotenv import load_dotenv
from sqlalchemy import create_engine

load_dotenv()

DB_CONFIG = {
    "host": os.getenv("DB_HOST", "localhost"),
    "port": int(os.getenv("DB_PORT", 5432)),
    "database": os.getenv("DB_NAME", "invest"),
    "user": os.getenv("DB_USER", "invest_user"),
    "password": os.getenv("DB_PASSWORD", "secure_password_123"),
}


def get_psycopg2_conn():
    return psycopg2.connect(**DB_CONFIG)


def get_sqlalchemy_engine():
    return create_engine(
        f"postgresql://{DB_CONFIG['user']}:{DB_CONFIG['password']}@"
        f"{DB_CONFIG['host']}:{DB_CONFIG['port']}/{DB_CONFIG['database']}"
    )


def load_tickers(filename="tickers.txt"):
    with open(filename, "r") as f:
        return [line.strip().upper() for line in f if line.strip()]
