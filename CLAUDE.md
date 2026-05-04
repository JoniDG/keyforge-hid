# CLAUDE.md — keyforge-hid

## Objetivo
Capa de hardware: enumera dispositivos HID conectados, los identifica por VID/PID y emite eventos de input (press/release/encoder) hacia los consumers (typically `keyforge-core`).

## Scope
- Detección y enumeración de dispositivos HID.
- Lectura de input reports de un dispositivo target.
- Emisión de `InputEvent` (estructura del schema en `keyforge-protocol`).
- Filtrado / "secuestro" del dispositivo para que el SO no reciba esos eventos en paralelo.

**Fuera de scope:**
- No conoce bindings, perfiles ni acciones (eso es `keyforge-core`).
- No habla WebSocket (eso es responsabilidad del daemon en `keyforge-core`).
- No sabe nada de RGB ni output reports (eso vendrá en una fase posterior, posiblemente como subpaquete).

## Stack
- Go 1.24
- `github.com/karalabe/hid` (HID cross-platform) — a evaluar `github.com/sstallion/go-hid` como alternativa cuando arranquemos.
- testify (tests)
- mockery v2 (mocks)
- golangci-lint

## Layout
```
cmd/probe/main.go      → binario chiquito para probar detección y eventos en consola
internal/
  device/              → enumeración, identificación (VID/PID), apertura
  events/              → mapping de raw input reports a InputEvent
```

## Comandos
```bash
make build    # compila cmd/probe
make test     # unit tests con coverage
make cover    # reporte HTML de coverage
make lint     # golangci-lint
make probe    # corre cmd/probe (detecta y prints eventos)
```

## Reglas duras

### Identidad del owner
- 👤 **Identidad del owner:**
  - LICENSE / copyright / contacto público: **Jonathan Daniel Gomez** / `jonathan.d.gomez98@gmail.com`
  - Commits / GitHub: **JoniDG** / `jonathan.d.gomez98+github@gmail.com`
- 🚨 **Antes de cada commit y push:** verificar `git config user.name` = `JoniDG` y `git config user.email` = `jonathan.d.gomez98+github@gmail.com`.

### Tipado
- ❌ **Prohibido** `interface{}` y `any`. El linter `forbidigo` lo rechaza.
- Si necesitás polimorfismo → interface concreta con métodos tipados.

### Tests
- Coverage **mínimo 95%** en `internal/`. Target: ~100%.
- `cmd/` queda excluido (entrypoints triviales).
- Framework: `testify`. Naming: `Test<Type>_<Method>_When<Condition>_Should<Result>`.
- Mocks: `mockery` en subpaquetes `mocks/`.

### Capas
- `device/` no conoce `events/` ni viceversa salvo vía interfaces explícitas.
- `cmd/probe/main.go` solo hace wiring — sin lógica.

### Errores
- Wrapping obligatorio: `fmt.Errorf("device.Open: %w", err)`.
- Errores centinela en `internal/device/errors.go` cuando aparezcan.

### Cross-platform
- Cualquier código OS-specific va en archivos con build tags (`//go:build linux`, etc).
- Tests deben pasar en al menos uno de los 3 SOs sin requerir hardware específico (usar mocks).

## Para Claude — cómo ayudarme acá

- Antes de proponer una librería HID, **chequeá Context7** para ver las APIs actuales (Go HID landscape cambió varias veces).
- Cuando agregues código que toque el SO directamente, **separalo con build tags** y dejá una stub testeable para los otros SOs.
- Después de cada cambio: `make test` debe pasar y coverage no bajar de 95%.
- No agregues deps a `go.mod` sin preguntar.

## Referencias

- Contexto cross-repo: [`../CLAUDE.md`](../CLAUDE.md)
- Schemas de eventos: `../keyforge-protocol/schemas/common.schema.json` ($defs/InputEvent)
- karalabe/hid: https://github.com/karalabe/hid
- sstallion/go-hid: https://github.com/sstallion/go-hid
