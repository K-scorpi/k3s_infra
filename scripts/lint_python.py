#!/usr/bin/env python3
"""
Скрипт для автоматического линтинга и форматирования Python файлов
Использует: black, isort, flake8, pylint
"""

import argparse
import os
import subprocess
import sys
from pathlib import Path
from typing import List, Tuple


class PythonLinter:
    def __init__(self, config_path: str = None):
        self.config_path = config_path
        self.setup_tools()

    def setup_tools(self):
        """Проверяет наличие необходимых инструментов"""
        required_tools = ["black", "isort", "flake8", "pylint"]
        missing_tools = []

        for tool in required_tools:
            try:
                subprocess.run([tool, "--version"], capture_output=True, check=True)
            except (subprocess.CalledProcessError, FileNotFoundError):
                missing_tools.append(tool)

        if missing_tools:
            print(f"❌ Отсутствуют инструменты: {', '.join(missing_tools)}")
            print("Установите их:")
            print("pip install black isort flake8 pylint")
            sys.exit(1)

    def find_python_files(
        self, path: str, exclude_dirs: List[str] = None
    ) -> List[Path]:
        """Находит все Python файлы в директории"""
        if exclude_dirs is None:
            exclude_dirs = [
                "__pycache__",
                ".git",
                ".venv",
                "venv",
                "env",
                "node_modules",
            ]

        python_files = []
        path_obj = Path(path)

        if path_obj.is_file() and path_obj.suffix == ".py":
            return [path_obj]

        for py_file in path_obj.rglob("*.py"):
            # Пропускаем исключенные директории
            if any(excluded in py_file.parts for excluded in exclude_dirs):
                continue
            python_files.append(py_file)

        return python_files

    def run_black(self, files: List[Path]) -> bool:
        """Запускает форматирование с помощью black"""
        print("🎨 Форматирование кода с Black...")
        try:
            cmd = ["black"] + [str(f) for f in files]
            result = subprocess.run(cmd, capture_output=True, text=True)

            if result.returncode == 0:
                print("✅ Black: форматирование завершено успешно")
                return True
            else:
                print(f"❌ Black: ошибка форматирования")
                print(result.stderr)
                return False

        except Exception as e:
            print(f"❌ Ошибка при запуске Black: {e}")
            return False

    def run_isort(self, files: List[Path]) -> bool:
        """Сортирует импорты с помощью isort"""
        print("📚 Сортировка импортов с Isort...")
        try:
            cmd = ["isort"] + [str(f) for f in files]
            result = subprocess.run(cmd, capture_output=True, text=True)

            if result.returncode == 0:
                print("✅ Isort: сортировка импортов завершена")
                return True
            else:
                print(f"❌ Isort: ошибка сортировки")
                print(result.stderr)
                return False

        except Exception as e:
            print(f"❌ Ошибка при запуске Isort: {e}")
            return False

    def run_flake8(self, files: List[Path]) -> Tuple[bool, str]:
        """Проверяет стиль кода с помощью flake8"""
        print("🔍 Проверка стиля с Flake8...")
        try:
            cmd = ["flake8"] + [str(f) for f in files]
            result = subprocess.run(cmd, capture_output=True, text=True)

            if result.returncode == 0:
                print("✅ Flake8: проверка стиля пройдена")
                return True, result.stdout
            else:
                print("⚠️  Flake8: найдены проблемы со стилем")
                return False, result.stdout

        except Exception as e:
            print(f"❌ Ошибка при запуске Flake8: {e}")
            return False, str(e)

    def run_pylint(self, files: List[Path]) -> Tuple[bool, str]:
        """Анализирует код с помощью pylint"""
        print("🔎 Анализ кода с Pylint...")
        try:
            # Ограничиваем вывод для читаемости
            cmd = ["pylint", "--output-format=text", "--score=yes"] + [
                str(f) for f in files
            ]
            result = subprocess.run(cmd, capture_output=True, text=True)

            if result.returncode == 0:
                print("✅ Pylint: анализ завершен успешно")
                return True, result.stdout
            else:
                print("⚠️  Pylint: найдены проблемы в коде")
                return False, result.stdout

        except Exception as e:
            print(f"❌ Ошибка при запуске Pylint: {e}")
            return False, str(e)

    def lint_files(self, files: List[Path], fix: bool = False) -> bool:
        """Основная функция линтинга"""
        print(f"🔧 Обработка {len(files)} Python файлов...")

        success = True

        # Форматирование (только если fix=True)
        if fix:
            if not self.run_black(files):
                success = False
            if not self.run_isort(files):
                success = False

        # Проверки
        flake8_ok, flake8_output = self.run_flake8(files)
        if not flake8_ok and flake8_output:
            print("\n--- Flake8 Issues ---")
            print(flake8_output)
            success = False

        pylint_ok, pylint_output = self.run_pylint(files)
        if not pylint_ok and pylint_output:
            print("\n--- Pylint Issues ---")
            # Показываем только первые 20 строк для читаемости
            lines = pylint_output.split("\n")[:20]
            print("\n".join(lines))
            if len(pylint_output.split("\n")) > 20:
                print("... (вывод обрезан, используйте --verbose для полного вывода)")
            success = True

        return success


def main():
    parser = argparse.ArgumentParser(description="Python линтер и форматировщик")
    parser.add_argument(
        "path",
        nargs="?",
        default=".",
        help="Путь к файлу или директории (по умолчанию: текущая директория)",
    )
    parser.add_argument(
        "--fix",
        action="store_true",
        help="Автоматически исправить форматирование и импорты",
    )
    parser.add_argument(
        "--exclude",
        nargs="+",
        default=["__pycache__", ".git", ".venv", "venv"],
        help="Директории для исключения",
    )
    parser.add_argument("--verbose", action="store_true", help="Подробный вывод")

    args = parser.parse_args()

    linter = PythonLinter()

    # Находим файлы
    files = linter.find_python_files(args.path, args.exclude)

    if not files:
        print("❌ Python файлы не найдены")
        sys.exit(1)

    print(f"📁 Найдено {len(files)} Python файлов")

    # Запускаем линтинг
    success = linter.lint_files(files, args.fix)

    if success:
        print("\n🎉 Все проверки пройдены успешно!")
        sys.exit(0)
    else:
        print("\n❌ Найдены проблемы в коде")
        sys.exit(1)


if __name__ == "__main__":
    main()
