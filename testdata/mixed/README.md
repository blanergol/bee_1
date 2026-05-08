# testdata/mixed

Фикстуры для ручной проверки фильтров: числа НЕ должны попадать в результат, бинарные файлы должны пропускаться.

## Файлы

- `article.txt` — обычный текст с числами и алфавитно-цифровыми токенами.
- `notes.txt` — версии и даты среди обычных слов.
- `payload.bin` — бинарь с NUL-байтами и ASCII-фрагментами; должен быть пропущен sniff'ом.
- `fake.png` — PNG-подобный заголовок с NUL-байтами и ASCII-фрагментом; тоже должен быть пропущен.

## Что ожидать

```bash
go run ./cmd/topwords --dir ./testdata/mixed --min-length 3 --top 20 --log-level info
```

С `--log-level info` в `stderr` будут две строки вида:

```
... msg="counter: skipping non-text file" path=testdata/mixed/fake.png
... msg="counter: skipping non-text file" path=testdata/mixed/payload.bin
```

В выдаче на `stdout` НЕ должно быть:

- чистых чисел из `article.txt` и `notes.txt` (они отфильтрованы регэкспом);
- ASCII-фрагментов из `payload.bin` и `fake.png` (бинарные файлы пропущены sniff'ом — слова оттуда вообще не попадают в подсчёт).

В выдаче должны быть обычные слова и алфавитно-цифровые токены вроде `mp3`, `h2o`, `ipv4`, `well-known`.
