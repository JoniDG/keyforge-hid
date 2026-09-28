# CLAUDE.md — keyforge-hid

## Objetivo
Capa de hardware: enumera dispositivos HID conectados, los identifica por VID/PID y emite eventos de input (press/release/encoder) hacia los consumers (typically `keyforge-core`).

## Scope
- Detección y enumeración de dispositivos HID.
- Lectura de input reports de un dispositivo target.
- Emisión de `InputEvent` (estructura del schema en `keyforge-protocol`).
- Filtrado / "secuestro" del dispositivo para que el SO no reciba esos eventos en paralelo.
- Driver de la interface vendor del keypad (`internal/vendor/`): leer/escribir qué código emite cada input físico, efecto de luz y color por tecla.

**Fuera de scope:**
- No conoce bindings, perfiles ni acciones (eso es `keyforge-core`).
- No habla WebSocket (eso es responsabilidad del daemon en `keyforge-core`).
- Qué acción dispara cada código que emite un input (eso es `keyforge-core`).
- API genérica de RGB ("color del input X"): la define core (7.b). Acá solo vive el driver de bajo nivel del keypad.

## Stack
- Go 1.24
- `github.com/karalabe/hid` (HID cross-platform) — a evaluar `github.com/sstallion/go-hid` como alternativa cuando arranquemos.
- testify (tests)
- mockery v2 (mocks)
- golangci-lint

## Layout
```
source.go              → API pública (package hid): Discover() + Stream(), envuelve el pipeline interno
cmd/probe/main.go      → binario chiquito para probar detección y eventos en consola
internal/
  device/              → enumeración, identificación (VID/PID), apertura
  events/              → mapping de raw input reports a InputEvent
  vendor/              → driver de la interface vendor (FF00) del keypad: leer/escribir slots de input, efecto y color RGB
```

## API pública
El package raíz `hid` (`github.com/JoniDG/keyforge-hid`) es la única superficie importable por otros módulos (p.ej. `keyforge-core`). `internal/` no es importable desde afuera por diseño.

- `hid.New(opts ...Option) *Source` — wirea el pipeline real (enumerator + identifier con `DefaultRegistry` + opener).
- `Source.Discover() (Device, error)` — primer device reconocido; `Device.ID` es el `protocol.DeviceID` que van a llevar los eventos (lo necesita core para armar bindings antes de streamear). Devuelve `ErrNoRecognizedDevice` si no hay ninguno conectado.
- `Source.DiscoverDevice() (protocol.Device, error)` — el mismo device reconocido como `protocol.Device` completo (id, `vendor_id`/`product_id` en hex lowercase de 4 dígitos, `path`, `manufacturer`/`product`/`serial_number` opcionales, e `inputs` con el catálogo de inputs lógicos). Pensado para que el daemon de core persista el device en `devices.json` y lo sirva por `list_devices` **sin** abrir/streamear. Mismo `ErrNoRecognizedDevice` cuando no hay keypad. El catálogo `inputs` es curado por device (campo `Controls` en `internal/device/known.go`) y describe el keypad **provisionado** (ver `Provision`): `key_0x68`…`key_0x71` ("Key 1…10", F13–F22) y `encoder_0`/`encoder_1` ("Encoder 1/2"). Los labels van numerados en la orientación canónica (horizontal, encoders a la derecha, teclas 1–5 arriba) y no dependen de cómo se use el keypad; dibujarlo rotado es cosa de la GUI. Cada `id` matchea el `input_id` que emiten los mappers.
- `Source.Provision(ctx context.Context) error` — escribe el layout KeyForge (`KnownDevice.Vendor.Layout`) en el keypad vía la interface vendor para que cada tecla y encoder emita un código distinto. Persiste en el device, solo escribe los slots que difieren (se puede correr N veces), no necesita `sudo` en macOS y puede correr con `Stream` activo. Devuelve `ErrNoRecognizedDevice` sin keypad y `ErrNoVendorInterface` si el device no tiene interface vendor. El `ctx` acota toda la llamada (resolve + intercambio vendor): si se cancela o vence, corta, cierra la interface y devuelve `ctx.Err()` envuelto. La cancelación es cooperativa (se chequea antes de cada write y en cada poll del ack, ~50 ms); un `OpenPath`/`Write` trabado dentro de hidapi no se puede interrumpir. **Sin provisionar, el keypad emite el chord de fábrica (`mod_lctrl` + `key_0x04`) y los dos encoders colapsan en `encoder_0`**, que no están en el catálogo.
- `Source.Stream(ctx, func(protocol.InputEvent) error) error` — bloquea hasta cancelación del ctx (devuelve `nil`), error del sink, o fallo de un reader (ambos se propagan).
- `WithSeize(bool)` — opción de seize; **default `false`** (sin privilegios). Con `true`, `Stream` falla si la plataforma no soporta seize en vez de degradar a shared en silencio: el error envuelve `hid.ErrSeizeNotImplemented` (alias de `device.ErrSeizeNotImplemented`) para que core lo distinga con `errors.Is` y reintente en shared.
- `hid.SeizeSupport() (supported bool, note string)` — si el build actual acepta `WithSeize(true)` (no falla con `ErrSeizeNotImplemented`), más una nota de una línea para logs/UI.

Los seams (`enumerator`/`identifier`/`opener`/`setSeize`) son inyectables vía fields no exportados para que los tests corran sin hidapi/cgo (CI no tiene hardware).

## Comandos
```bash
make build    # compila cmd/probe
make test     # unit tests con coverage
make cover    # reporte HTML de coverage
make lint     # golangci-lint
make probe    # corre cmd/probe (detecta y prints eventos)
```

`probe` también habla con la interface vendor (sin `sudo` en macOS): `-provision` escribe el layout KeyForge y `-factory-layout` lo deshace (vuelve a `Ctrl+A` en todas las teclas), `-vendor-slots` lee los slots de input de la capa 0, `-vendor-effect <off|static|breath|trigger|spectrum|user>` cambia el efecto de luz, y `-vendor-color LED:RRGGBB` prende una tecla (pasa el efecto a `user` primero). Los cambios persisten en el keypad; `-vendor-effect spectrum` vuelve al arcoíris de fábrica.

### Driver vendor (`internal/vendor/`)
- Protocolo documentado en `docs/hid-device-keyforge-keypad.md` §4.3.
- **Regla dura:** el paquete solo arma los 4 comandos verificados (leer/escribir slot, efecto, color). **No** implementar el factory reset (`06 0F FF`) ni la entrada al bootloader (`5A A0`), ni exportar un "send raw". Un test lo verifica.
- Escribir slots es persistente en el device: cualquier cambio de mapeo tiene que poder revertirse escribiendo los valores de fábrica slot por slot.
- El cliente solo acepta índices dentro de los rangos verificados (`KnownDevice.Vendor.Slots`/`LEDs`) y solo trabaja sobre la capa 0 (las demás capas no están probadas). El firmware hace ack de cualquier índice, así que el límite lo pone el driver.
- Un ack solo cuenta si además repite el payload enviado, para que un ack tardío de un comando anterior no se tome como confirmación.
- `vendor.Open` recibe la función de apertura; el wiring le pasa siempre `device.OpenPath`. **Regla dura:** ninguna apertura de hidapi fuera de `internal/device` llama a `hid.OpenPath` directo. `device.OpenPath` abre bajo el mismo lock que la enumeración y con el modo de `SetSeize`; un `hid.OpenPath` suelto puede correr el `hid_init` implícito de hidapi, que en macOS resetea el modo a seize. La interface vendor (FF00) no es un teclado: en macOS abre sin `sudo` tanto en seize como en shared, así que `Provision` hereda el modo del proceso sin problema.

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
