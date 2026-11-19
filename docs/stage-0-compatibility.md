# Этап 0: подготовка контура и проверка совместимости

- **Дата начала:** 18.11.2026
- **Дата завершения:** 19.11.2026
- **Статус:** Завершён; полный live smoke-test пройден
- **Проверенная версия Proxmox VE:** 9.2.10 (`pve-manager/9.2.10/43df2e01f27a1a19`)
- **Правило безопасности:** изменения допускались только для ресурсов выделенного тестового pool

## 1. Подтверждённые версии

| Компонент | Версия | Результат |
|---|---:|---|
| Proxmox VE | 9.2.10 | API и гостевые операции работают |
| OpenTofu | 1.13.9 | `init`, `validate`, `plan`, `apply`, `destroy` пройдены |
| `bpg/proxmox` | 0.123.0 | QEMU и LXC create/update/delete пройдены |
| Ansible core | 2.19.11 | Установлен на Runner; гостевые playbooks в smoke-test не проверялись |
| Git | 2.47.3 | Установлен на Runner |

Provider установлен из официального GitHub release с проверкой SHA-256 в локальный filesystem mirror Runner. Предупреждение OpenTofu `(unauthenticated)` относится к этому способу установки и не означает пропуск проверки архива.

Обновление provider в пределах 0.x без повторной проверки совместимости не допускается.

Источники:

- https://bpg.sh/docs/
- https://github.com/bpg/terraform-provider-proxmox
- https://github.com/bpg/terraform-provider-proxmox/releases
- https://github.com/opentofu/opentofu/releases

## 2. Фактически проверенные операции без SSH к Proxmox-хосту

1. Чтение версии, нод, storage, pools, сети и списка гостевых ресурсов через API.
2. Полное клонирование QEMU из заранее подготовленного Cloud-Init Template.
3. Запуск QEMU и получение DHCP-адреса через QEMU Guest Agent.
4. Подключение к QEMU по SSH с использованием Cloud-Init SSH key.
5. Изменение CPU и RAM QEMU на месте: 2 → 3 CPU, 2048 → 3072 MB.
6. Создание непривилегированного LXC из готового Debian 13 template.
7. Запуск LXC, получение DHCP-адреса и подключение по SSH.
8. Изменение CPU и RAM LXC на месте: 2 → 3 CPU, 1024 → 2048 MB.
9. Повторный `tofu plan` без расхождений после изменений.
10. Удаление только созданных QEMU `9201` и LXC `9202`; state очищен.

Для OpenTofu использовался только Proxmox API token. SSH к Proxmox-хосту в конфигурации provider отсутствовал.

## 3. Параметры проверенного контура

Секретные значения токенов в документ и Git не записываются.

| Параметр | Значение |
|---|---|
| Proxmox API endpoint | `https://192.168.80.149:8006/` |
| TLS | Самоподписанный сертификат; `insecure = true` разрешён только как исключение этапа 0 |
| Нода | `pve`, online; 48 CPU, около 270 GB RAM |
| Тестовый pool | `orchestrator-test`, создан |
| Системный pool | `orchestrator-system`, создан; содержит Runner |
| Storage для новых дисков | `raid_1_2tb`; поддерживает `images` и `rootdir` |
| Storage LXC template | `NVME_1TB_sec`; в smoke-test только чтение |
| Bridge | `vmbr0` |
| Сеть | `192.168.80.0/24`, gateway `192.168.80.1` |
| DHCP | Проверен на QEMU и LXC |
| Исходный VM Template | VMID `9000`; не использовать для новых DHCP-клонов без очистки |
| Очищенный VM Template | VMID `9001`, `orchestrator-debian13-clean`; проверен |
| LXC Template | `NVME_1TB_sec:vztmpl/debian-13-standard_13.6-1_amd64.tar.zst` |
| Read-only token | `orchestrator-ro@pve!Audit`; проверен |
| Write token | `orchestrator-rw@pve!OpenTofu`; проверен на create/update/delete |
| Runner | VMID `9100`, `orchestrator-runner`, `192.168.80.179` |
| SSH к Runner | Ключ `orchestrator-runner`, пользователь `orchestrator`, `IdentitiesOnly=yes` |

### Существующие ресурсы — не изменять

- LXC `100` (`homework`);
- LXC `101` (`SQLtandemDON61`);
- QEMU `104` (`windows`);
- QEMU `107` (`test-oc`);
- Runner `9100` и подготовленные templates не относятся к удаляемым smoke-ресурсам.

`NVME_1TB_sec` заполнено примерно на 85 %, поэтому новые диски размещались на `raid_1_2tb`.

## 4. Модель доступа, подтверждённая smoke-test

Используется отдельный пользователь `orchestrator-rw@pve`. У API token `OpenTofu` выключено Privilege Separation, поэтому действуют ACL пользователя. Роль Administrator не назначалась.

| Path | Role / назначение |
|---|---|
| `/pool/orchestrator-test` | `PVEVMAdmin` и `PVEPoolAdmin` |
| `/storage/raid_1_2tb` | `PVEDatastoreUser` |
| `/storage/NVME_1TB_sec` | `PVEAuditor` |
| `/nodes/pve` | `PVEAuditor` |
| `/sdn/zones/localnetwork/vmbr0` | `PVESDNUser` |
| `/vms/9202` | `PVEVMAdmin` на время создания тестового LXC |

Важные особенности PVE 9.2.10:

- добавление QEMU в pool потребовало `Pool.Allocate`; предоставлен через `PVEPoolAdmin` только на тестовом pool;
- при создании LXC проверка `VM.Config.Options` выполнялась по `/vms/9202` и не была удовлетворена одной ACL на pool;
- для будущего приложения нельзя бездумно выдавать `PVEVMAdmin` на весь `/vms`: это даст доступ к чужим ВМ;
- генератор должен использовать предсказуемые VMID и согласованную модель предварительных per-VM ACL либо отдельно спроектированный provisioning workflow.

## 5. Найденные проблемы и решения

### Одинаковый DHCP-адрес у QEMU-клонов

Первый клон получил `192.168.80.179`, совпадающий с адресом Runner. MAC-адреса и SMBIOS UUID при этом различались. Причина — исходный template `9000` сохранил `/etc/machine-id` после подготовки.

Решение:

1. Создан отдельный клон `9001`.
2. В гостевой системе выполнены `cloud-init clean --logs --machine-id`, удалены SSH host keys и DHCP leases.
3. Клон выключен и преобразован в template.
4. Тестовая QEMU `9201` пересоздана из `9001` и получила отдельный адрес `192.168.80.181`.
5. `machine-id` новой ВМ отличался от Runner; SSH host fingerprint также был отдельным.

Template `9001` является проверенной основой для последующих QEMU-клонов. Процедура подготовки новых templates должна всегда очищать machine identity.

### LXC Systemd 257 warning

Provider выводил предупреждение `Systemd 257 detected. You may need to enable nesting.` Базовые запуск, SSH, CPU/RAM update и destroy прошли без `nesting = true`. Для будущих workloads, которым действительно нужен nesting, это должно быть отдельным явным параметром дизайна, а не разрешением по умолчанию.

## 6. Операции, исключённые из smoke-test

- загрузка custom Cloud-Init snippets через provider;
- LXC `idmap`;
- bind mounts и feature flags privileged LXC;
- USB/PCI passthrough без resource mapping;
- virtiofs к реальным устройствам;
- импорт локального диска через `source_file.path`;
- `root_ticket`, root SSH и сохранение root-пароля;
- изменение или удаление рабочих ресурсов вне тестового pool.

Эти операции не требуются для первой версии конструктора и не должны неявно появляться в генерируемом HCL.

## 7. Итоговый чек-лист

- [x] Проверена заявленная совместимость по официальной документации.
- [x] Получены параметры тестового контура.
- [x] Подготовлены pools, users, tokens и templates.
- [x] Подготовлена изолированная Runner VM.
- [x] Установлены и зафиксированы OpenTofu и `bpg/proxmox`.
- [x] Выполнен read-only API test.
- [x] Выполнен полный QEMU и LXC live smoke-test на PVE 9.2.10.
- [x] Проверены DHCP, SSH и уникальность machine identity.
- [x] Проверены изменения CPU/RAM и повторный план без расхождений.
- [x] Удалены только `9201` и `9202`.
- [x] Ни одна OpenTofu-операция не использовала SSH к Proxmox-хосту.
- [x] Фактические результаты и ограничения записаны в отчёт.

## 8. Отдельные задачи гигиены контура

Эти задачи не отменяют результат smoke-test и выполняются отдельно:

- проверить удаление случайно установленных `tofu` и Ansible с Proxmox-хоста; рабочие инструменты должны оставаться на Runner;
- удалить ошибочно созданный личный token `Uthstar01@pam!OpenTofu`, если он больше не нужен;
- перед производственным использованием заменить `insecure = true` на доверенную TLS-проверку;
- Ansible guest configuration проверить отдельным тестом на этапе разработки приложения.
