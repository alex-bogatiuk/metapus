# Тестовый Docker-стенд: TRON Nile и Ethereum Sepolia

Основная инструкция по публичному стенду: [public-nile-deployment.md](../docs/operations/public-nile-deployment.md).

Для нового disposable state из корня проекта запустите:

```powershell
./scripts/dev/run-docker-stand.ps1 -StateDir "<новой-пустой-каталог>" -PublicIP "<IP-вашего-стенда>" -AdminPassword "<тестовый-пароль>"
```

Скрипт создаёт один tenant и раздельные Vault custody mounts для TRON и EVM,
выводит по четыре управляемых кошелька на сеть, настраивает RPC, fee wallets,
wallet pools и политики сети, выполняет preflight для обеих сетей и запускает
API, worker и frontend. Адреса сохраняются вне репозитория в
`wallets-tron.json` и `wallets-evm.json` внутри state directory.

**Сетевые политики остаются выключенными до финансирования.** Подтверждённые
native funding транзакции hot и fee wallets нужно учесть через
`crypto-record-wallet-funding`, повторить `configure-db` и preflight. Затем
оператор включает каждую сеть в Admin UI после MFA step-up. Скрипт не меняет
остатки или статусы активации прямым SQL. Для теста вывода требуется также
тестовая ликвидность USDT Nile либо PYUSD Sepolia.

Frontend в публичном Compose не публикует порт `3000`. Внешний доступ
настраивается через public edge и HTTPS по основной инструкции. Пароль
администратора находится в `secrets/admin_password` выбранного state directory;
скрипт его не выводит. На новом tenant потребуется первичная настройка MFA.
