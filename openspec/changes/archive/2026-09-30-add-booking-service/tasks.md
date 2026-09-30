# Tasks

Наблюдаемый red каждой тестовой задачи и каждого mutation-check записывается строкой под задачей (фрагмент реального вывода `go test`).

Откат мутаций — по процедуре из `.claude/constitution/20-testing.md`: копия файла в scratchpad до мутации, возврат из копии, сверка `cmp`, зелёный прогон.

## 1. Хранилище: создание и конфликт

- [x] 1.1 Создать `go.mod` (`go mod init booking`, затем `go mod edit -go=1.26`) и написать `internal/booking/store_test.go`: table-driven `TestStoreCreate_*` — успешное создание (непустой id, моменты в UTC); `ErrInvalid` для room `""`, `"   "`, `"\t\n"`, `"\u00a0"` и для end == start, end раньше start на 1 нс; room `"\u200b"`, `" g "` — успех; `ErrConflict` для частичных пересечений, вложенной, охватывающей, совпадающей брони, пересечения на 1 с у каждого края; касание с обеих сторон и брони комнат `"Green"`, `" green"`, `"blue"` — успех; после каждой ошибки число броней в хранилище не изменилось (проверка через `errors.Is`). Проверка: `go test ./internal/booking/` падает компиляцией — `NewStore`/`ErrInvalid`/`ErrConflict` не определены; вывод записан.
  - red: `store_test.go:18:39: undefined: Store` … `store_test.go:99:23: undefined: ErrInvalid` … `store_test.go:150:23: undefined: ErrConflict` → `FAIL booking/internal/booking [build failed]`
- [x] 1.2 Реализовать `internal/booking/booking.go` и `store.go` (`Booking`, `Store`, `NewStore`, `Create`, `ErrInvalid`, `ErrConflict`; проверка и вставка под одним захватом мьютекса; id — `crypto/rand.Text()`) по design.md. Проверка: `go test -race ./internal/booking/` зелёный.

## 2. Хранилище: выдача за сутки

- [x] 2.1 Дописать в `store_test.go` table-driven `TestStoreListDay_*`: все строки таблицы сценария «Попадание брони в сутки на границах суток»; бронь через полночь видна в обоих сутках; результат отсортирован по `Start` при вставке не по порядку; брони других комнат (`"Green"`, `"green "`) не попадают; пустой результат — не nil и длины 0; изменение возвращённого среза не меняет последующую выдачу. Проверка: падение компиляцией — `ListDay` не определён; вывод записан.
  - red: `store_test.go:222:13: s.ListDay undefined (type *Store has no field or method ListDay)` (×7) → `FAIL booking/internal/booking [build failed]`
- [x] 2.2 Реализовать `Store.ListDay(room, from, to)` (копия под блокировкой, фильтр `[from, to)`, `slices.SortFunc` по `Start.Compare`). Проверка: `go test -race ./internal/booking/` зелёный.
- [x] 2.3 Mutation-check окна суток: `e.Start.Before(to)` → `!e.Start.After(to)` — падает строка «начинается ровно в конце суток»; `from.Before(e.End)` → `!from.After(e.End)` — падает «заканчивается ровно в начале суток». Каждую мутацию откатить по процедуре из шапки. Проверка: оба red записаны, после отката `cmp` без различий и прогон зелёный.
  - Правило пересечения вынесено в общий `overlaps(aStart, aEnd, bStart, bEnd)` (конфликт и окно суток — одна формула), мутации внесены в него.
  - red 1 (`aStart.Before(bEnd)` → `!aStart.After(bEnd)`): `TestStoreListDay_DayBoundaries_MatchesHalfOpenDay/начинается_ровно_в_конце_суток`: `ListDay = [{… Start:2027-11-02 00:00:00 +0000 UTC …}], want empty`; заодно `TestStoreCreate_TouchingOrOtherRoom_Succeeds/касание_после_существующей`: `Create: booking overlaps an existing booking`.
  - red 2 (`bStart.Before(aEnd)` → `!bStart.After(aEnd)`): `…/заканчивается_ровно_в_начале_суток`: `ListDay = [{… End:2027-11-01 00:00:00 +0000 UTC}], want empty`; заодно `…/касание_перед_существующей`.
  - откат: `cmp: identical` после каждой мутации; `go test -race -count=1 ./internal/booking/` → `ok`.

## 3. HTTP: выдача броней по комнате и дате

- [x] 3.1 Написать `internal/booking/handler_test.go`: хелперы `do(t, h, method, target, body)`, `listDay`, `assertErrorResponse(t, rec, status)` (статус, `Content-Type: application/json`, непустой строковый `error`), `storeSize(s)`; брони в Given создаются через `s.Create`, запросы — через `NewHandler(s)` и `httptest.NewRecorder`. Тесты на все сценарии требования «Выдача броней по комнате и дате»: `TestListBookings_DayBoundaries_MatchesHalfOpenDay`, `TestListBookings_AcrossMidnight_VisibleInBothDays`, `TestListBookings_EndsAtMidnight_NotInNextDay`, `TestListBookings_Unordered_SortedByStart`, `TestListBookings_OtherRooms_Excluded`, `TestListBookings_ExtraQueryParams_Ignored`, `TestListBookings_NoBookings_EmptyArray` (сырое значение `bookings` равно `[]`), `TestListBookings_InvalidParams_Returns400`. Проверка: падение компиляцией — `NewHandler` не определён; вывод записан.
  - `storeSize` и `mustStoreCreate` уже есть в `store_test.go` (тот же пакет) — переиспользованы; добавлен `decodeBooking`, проверяющий ровно четыре строковых поля брони в каждом ответе.
  - red: `handler_test.go:128:22: undefined: NewHandler` (×7) → `FAIL booking/internal/booking [build failed]`
- [x] 3.2 Реализовать в `internal/booking/handler.go` `NewHandler` с веткой GET (разбор `room` через общую проверку «пустая после `strings.TrimSpace`», `date` через `time.DateOnly`), `writeJSON`, ответ-ошибку `{"error": …}`, представление брони с `RFC3339Nano`. Проверка: `go test -race ./internal/booking/` зелёный.
- [x] 3.3 Mutation-check: вернуть nil-срез вместо пустого в выдаче — падает `TestListBookings_NoBookings_EmptyArray` (`null` вместо `[]`). Откат по процедуре из шапки. Проверка: red записан, откат подтверждён `cmp`, прогон зелёный.
  - мутация в `handler.list`: `listResponse{Bookings: make([]bookingJSON, 0, …)}` → `var res listResponse`.
  - red: `TestListBookings_NoBookings_EmptyArray/броней_нет`: `handler_test.go:260: bookings = null, want []` (и два других подтеста).
  - откат: `cmp: identical`; `go test -race -count=1 ./internal/booking/` → `ok`.

## 4. HTTP: создание брони и формат брони

- [x] 4.1 Дописать в `handler_test.go` хелпер `mustCreate` и тесты на все сценарии требований «Формат брони в ответах» и «Создание брони»: `TestCreateBooking_Valid_ReturnsExactlyBookingFields`, `TestCreateBooking_ZeroOffset_NormalizedToZ`, `TestCreateBooking_FractionalSeconds_Preserved`, `TestCreateBooking_Several_UniqueIDs`, `TestCreateBooking_Valid_Returns201AndListed`, `TestCreateBooking_RoomVerbatim_StoredAsIs`, `TestCreateBooking_SingleNonSpaceRune_Returns201`, `TestCreateBooking_ExtraFields_Ignored`, `TestCreateBooking_PastAndAnyDuration_Returns201`, `TestCreateBooking_NotJSONObject_Returns400`, `TestCreateBooking_InvalidRoom_Returns400`, `TestCreateBooking_InvalidTimes_Returns400`; в тестах на 400 — `storeSize` не изменился и существующая бронь в выдаче с прежними id, start, end. Проверка: тесты падают на утверждениях (POST не обрабатывается); вывод записан.
  - red (9 тестов на 201): `handler_test.go:330: status = 400, want 201; body: {"error":"query parameter room is required and must not be blank"}` — POST уходит в единственную ветку GET.
  - Три теста на 400 (`NotJSONObject`, `InvalidRoom`, `InvalidTimes`) до 4.2 зелёные случайно: ветка GET отвечает 400 на отсутствие `room` в query. Их защитная сила проверяется мутациями в 4.3, включая добавленные: снять проверку пустой комнаты и проверку `end > start` в `Store.Create`.
- [x] 4.2 Реализовать ветку POST: `io.ReadAll` → `json.Unmarshal` в `map[string]json.RawMessage` → поля по точному ключу (`null` = отсутствует) → `time.Parse(time.RFC3339)` + отказ при ненулевом смещении → `Store.Create`; `ErrInvalid` → 400, прочие ошибки → 500 с JSON-телом (409 — в группе 5); ответ 201 с бронью. Проверка: `go test -race ./internal/booking/` зелёный.
- [x] 4.3 Mutation-check, каждую мутацию откатить по процедуре из шапки: `json.Unmarshal` → `json.NewDecoder(…).Decode` — падают «мусор после объекта» и «два JSON-значения»; разбор в структуру с тегами вместо карты — падают кейсы `"ROOM": "green"` и `"Room": "blue"`; `RFC3339Nano` → `RFC3339` — падает `TestCreateBooking_FractionalSeconds_Preserved`; удалить проверку смещения — падают кейсы `+03:00`, `-05:00`, `+00:01`. Проверка: все red записаны, откаты подтверждены `cmp`, прогон зелёный.
  - Decoder: `…/мусор_после_объекта` и `…/два_JSON-значения`: `status = 201, want 400; body: {"id":…,"room":"green",…}`.
  - структура с тегами (`json.RawMessage`-поля): `TestCreateBooking_ExtraFields_Ignored`: `room = "blue", want "green"`; `TestCreateBooking_InvalidRoom_Returns400/ключ_в_другом_регистре`: `status = 201, want 400`.
  - без проверки смещения: `…/ненулевое_смещение_start` и `…/смещение_в_одну_минуту`: `status = 201, want 400`; `…/ненулевое_смещение_end`: `status = 500, want 400` (end 15:00Z пересекается с существующей бронью, маппинга 409 ещё нет).
  - `RFC3339`: `TestCreateBooking_FractionalSeconds_Preserved`: `Start:2027-11-01T09:00:00Z … want … Start:2027-11-01T09:00:00.5Z`; заодно `…/длительность_1_наносекунда`: `interval = …09:00:00Z–…09:00:00Z`.
  - добавлено (см. 4.1) — без проверки пустой комнаты в `Create`: `TestCreateBooking_InvalidRoom_Returns400/{пустая_строка,только_пробелы,табуляция_и_перевод_строки,неразрывный_пробел}`: `status = 201, want 400`; `TestStoreCreate_Invalid_ReturnsErrInvalid/…`: `err = <nil>, want ErrInvalid`.
  - добавлено (см. 4.1) — без проверки `end > start`: `TestCreateBooking_InvalidTimes_Returns400/{end_равен_start,end_равен_start_в_другой_записи,end_раньше_start_на_1_наносекунду,end_раньше_start_на_час}`: `status = 201, want 400`; store-тесты — `err = <nil>, want ErrInvalid`.
  - Не получили отдельного red строки `InvalidTimes` про отсутствие, `null`, тип и формат строки (`2027-11-01`, пробел вместо T, строчные t/z, `+0000`, `2027-02-30`): их отклоняют `stringField` и `time.Parse`, осмысленной точечной мутации нет.
  - откат: `cmp` без различий по `handler.go` и `store.go` после каждой мутации; `go test -race -count=1 ./internal/booking/` → `ok`.

## 5. HTTP: конфликт броней и конкурентность

- [x] 5.1 Дописать в `handler_test.go` тесты на все сценарии требования «Конфликт броней»: `TestCreateBooking_Overlap_Returns409`, `TestCreateBooking_Touching_Returns201`, `TestCreateBooking_SubsecondBoundary_ConflictsPrecisely`, `TestCreateBooking_OtherRooms_NoConflict`, `TestCreateBooking_ConcurrentIdentical_ExactlyOneCreated`, `TestCreateBooking_ConcurrentOverlapping_ExactlyOneCreated` (50 горутин через `wg.Go`, старт по закрытию канала-барьера, результаты в срез по индексу, утверждения после `wg.Wait()`). Проверка: тесты на 409 падают на утверждениях — конфликт отдаётся как 500; вывод записан.
  - red: `TestCreateBooking_Overlap_Returns409` (9 подтестов), `…_SubsecondBoundary_ConflictsPrecisely`, `…_ConcurrentIdentical_ExactlyOneCreated`, `…_ConcurrentOverlapping_ExactlyOneCreated`: `status = 500, want 409; body: {"error":"internal error"}`.
  - `…_Touching_Returns201` и `…_OtherRooms_NoConflict` проверяют успешные 201 и зелёные уже до 5.2; их защита подтверждается мутацией правила пересечения в 5.3.
- [x] 5.2 Добавить маппинг `errors.Is(err, ErrConflict)` → 409. Проверка: `make test` (с `-race`) зелёный.
- [x] 5.3 Mutation-check, каждую мутацию откатить по процедуре из шапки: `Before` → `!After` в правиле пересечения — падает `TestCreateBooking_Touching_Returns201` (и касание в `TestStore_Create_*`); проверка и вставка под разными захватами мьютекса — тесты конкурентности дают больше одного 201 (при необходимости `go test -race -count=20 -run Concurrent ./internal/booking/`); удалить мьютекс — `-race` сообщает `DATA RACE`. Проверка: все red записаны, откаты подтверждены `cmp`, `make test` зелёный.
  - `<` → `<=` (`aStart.Before(bEnd)` → `!aStart.After(bEnd)` в `overlaps`): `TestCreateBooking_Touching_Returns201`: `POST "green" 2027-11-01T10:00:00Z–2027-11-01T11:00:00Z: status = 409, want 201`; `…_SubsecondBoundary_ConflictsPrecisely`: касание `10:00:00.5Z` → `409, want 201`; также `…_Several_UniqueIDs`, `TestStoreCreate_TouchingOrOtherRoom_Succeeds/касание_после_существующей`, окно суток в store- и HTTP-тестах.
  - проверка и вставка под разными захватами: `TestCreateBooking_ConcurrentIdentical_ExactlyOneCreated`: `created 2 bookings, want exactly 1`. Ловится вероятностно: при `-count=20` `Identical` упал 12 раз из 20, `Overlapping` — 9 из 20.
  - без мьютекса в `Create`: `WARNING: DATA RACE` ×3, `testing.go:1865: race detected during execution of test`, `--- FAIL: TestCreateBooking_ConcurrentIdentical_ExactlyOneCreated`.
  - добавлено (охрана `OtherRooms`) — конфликт по броням всех комнат: `TestCreateBooking_OtherRooms_NoConflict`: `POST "blue" …: status = 409, want 201`; `TestStoreCreate_TouchingOrOtherRoom_Succeeds/{другой_регистр_комнаты,пробел_перед_именем_комнаты,другая_комната}`; `…_OtherRooms_Excluded` на обоих уровнях.
  - откат: `cmp: identical` после каждой мутации; `make test` → `ok booking/internal/booking`.

## 6. HTTP: формат ошибок, 404 и 405

- [x] 6.1 Дописать в `handler_test.go` тесты на все сценарии требования «Формат ошибок»: `TestErrors_AllKinds_JSONFormat`, `TestBookings_UnsupportedMethod_Returns405` (заголовок `Allow` содержит GET и POST; бронь не изменена), `TestBookings_Head_Returns405`, `TestUnknownPath_Returns404` (включая `//bookings`, `/x/../bookings`, `/bookings/`; нет заголовка `Location`; брони не созданы). Проверка: тесты падают на утверждениях — неизвестные пути и методы обрабатываются как `/bookings` GET/POST; вывод записан.
  - red: `…/PUT_/bookings`: `status = 400, want 405`; `TestBookings_Head_Returns405`: `status = 200, want 405`; `TestUnknownPath_Returns404/GET_/bookings/123`: `status = 400, want 404`; `…/POST_/booking`: `status = 201, want 404`; `…/GET_//bookings?room=green&date=2027-11-01`: `status = 200, want 404`; `…/GET_/x/../bookings?…`: `status = 200, want 404`.
- [x] 6.2 Добавить в `NewHandler` проверку `r.URL.Path == "/bookings"` (иначе 404) и ветку по умолчанию — 405 с `Allow: GET, POST`; оба ответа через общий JSON-хелпер ошибок. Проверка: `make test` зелёный.

## 7. Точка входа

- [x] 7.1 Ручная проверка вместо теста (решение 8: PORT и хранение в памяти — без сценариев спеки). Зафиксировать red: `make run` падает, потому что `cmd/booking` не существует; вывод записан.
  - red: `PORT=8080 go run -race ./cmd/booking` → `stat …/cmd/booking: directory not found` → `make: *** [run] Error 1`.
- [x] 7.2 Реализовать `cmd/booking/main.go`: адрес `":" + cmp.Or(os.Getenv("PORT"), "8080")`, `http.Server` с `booking.NewHandler(booking.NewStore())`, `log.Fatal(srv.ListenAndServe())`. Проверка: `make run` в фоне → `curl` POST на `:8080` даёт 201, GET — 200 с бронью; `PORT=18080 make run` слушает `:18080`; после перезапуска GET отдаёт пустой массив; результаты записаны.
  - `make run`: `POST :8080/bookings` → `201 application/json` `{"id":"HKQDLMLJIEG3AVKFLDQ4OKOBVS","room":"green","start":"2027-11-01T09:00:00Z","end":"2027-11-01T10:00:00Z"}`; `GET :8080/bookings?room=green&date=2027-11-01` → `200 application/json` с этой бронью.
  - `PORT=18080 make run`: лог `listening on :18080`; `GET :18080/bookings?room=green&date=2027-11-01` → `200 application/json` `{"bookings":[]}` — после перезапуска данных нет; `:8080` не отвечает (`000`).
  - Процессы остановлены `kill` по слушающему порту (`make` завершился кодом 2 от сигнала — ожидаемо).

## 8. Самоаудит и итоговая проверка

- [x] 8.1 Самоаудит: таблица «сценарий спеки → тест (функция и подтест)» по всем 30 сценариям specs/bookings/spec.md записана сюда же, непокрытых сценариев нет.

  | # | Требование → сценарий | Тест (`internal/booking/handler_test.go`), подтесты |
  |---|---|---|
  | 1 | Формат → Ответ на создание содержит ровно поля брони | `TestCreateBooking_Valid_ReturnsExactlyBookingFields` |
  | 2 | Формат → Нулевое смещение в ответе приводится к суффиксу Z | `TestCreateBooking_ZeroOffset_NormalizedToZ` (`+00:00`, `-00:00`) |
  | 3 | Формат → Дробные секунды сохраняются в ответах | `TestCreateBooking_FractionalSeconds_Preserved` |
  | 4 | Формат → Каждая бронь получает собственный id | `TestCreateBooking_Several_UniqueIDs` |
  | 5 | Создание → Успешное создание брони | `TestCreateBooking_Valid_Returns201AndListed` |
  | 6 | Создание → Комната сохраняется в точности как пришла | `TestCreateBooking_RoomVerbatim_StoredAsIs` |
  | 7 | Создание → Комната из одного непробельного символа допустима | `TestCreateBooking_SingleNonSpaceRune_Returns201` (3) |
  | 8 | Создание → Лишние поля запроса игнорируются | `TestCreateBooking_ExtraFields_Ignored` |
  | 9 | Создание → Бронь в прошлом и любой длительности допустима | `TestCreateBooking_PastAndAnyDuration_Returns201` (3) |
  | 10 | Создание → Тело не является JSON-объектом | `TestCreateBooking_NotJSONObject_Returns400` (9) |
  | 11 | Создание → Невалидная комната | `TestCreateBooking_InvalidRoom_Returns400` (9) |
  | 12 | Создание → Невалидные start и end | `TestCreateBooking_InvalidTimes_Returns400` (19) |
  | 13 | Конфликт → Пересекающийся интервал отклоняется | `TestCreateBooking_Overlap_Returns409` (9) |
  | 14 | Конфликт → Касание интервалов не является конфликтом | `TestCreateBooking_Touching_Returns201` |
  | 15 | Конфликт → Граница пересечения с точностью до долей секунды | `TestCreateBooking_SubsecondBoundary_ConflictsPrecisely` |
  | 16 | Конфликт → Брони других комнат не учитываются | `TestCreateBooking_OtherRooms_NoConflict` |
  | 17 | Конфликт → Одновременные одинаковые запросы создают ровно одну бронь | `TestCreateBooking_ConcurrentIdentical_ExactlyOneCreated` |
  | 18 | Конфликт → Одновременные попарно пересекающиеся запросы создают ровно одну бронь | `TestCreateBooking_ConcurrentOverlapping_ExactlyOneCreated` |
  | 19 | Выдача → Попадание брони в сутки на границах суток | `TestListBookings_DayBoundaries_MatchesHalfOpenDay` (8) |
  | 20 | Выдача → Бронь через полночь видна в обоих сутках | `TestListBookings_AcrossMidnight_VisibleInBothDays` (4) |
  | 21 | Выдача → Бронь, заканчивающаяся ровно в 00:00, в следующих сутках не видна | `TestListBookings_EndsAtMidnight_NotInNextDay` |
  | 22 | Выдача → Брони упорядочены по возрастанию start | `TestListBookings_Unordered_SortedByStart` |
  | 23 | Выдача → Выдача только по указанной комнате | `TestListBookings_OtherRooms_Excluded` |
  | 24 | Выдача → Лишние query-параметры игнорируются | `TestListBookings_ExtraQueryParams_Ignored` |
  | 25 | Выдача → Нет броней — пустой массив | `TestListBookings_NoBookings_EmptyArray` (3) |
  | 26 | Выдача → Невалидные параметры выдачи | `TestListBookings_InvalidParams_Returns400` (13) |
  | 27 | Ошибки → Ошибки всех видов в едином формате | `TestErrors_AllKinds_JSONFormat` (5) |
  | 28 | Ошибки → Неподдерживаемый метод на /bookings | `TestBookings_UnsupportedMethod_Returns405` (4) |
  | 29 | Ошибки → HEAD на /bookings не поддерживается | `TestBookings_Head_Returns405` |
  | 30 | Ошибки → Неизвестный путь | `TestUnknownPath_Returns404` (7) |

  - Непокрытых сценариев нет. Шаги Then/And сверены с утверждениями тестов; единственный найденный пробел — в №16 не проверялось, что созданные брони имеют интервал 09:00–10:00, — закрыт проверкой в цикле создания.
  - Сверх спеки: 11 unit-тестов `TestStore*` в `store_test.go` (правило пересечения, окно суток, сортировка, копия результата, доменная валидация).
- [x] 8.2 Полный `make ai-check` после закрытия всех задач реализации: зелёный, реальный вывод записан.
  - Первый прогон упал на `make lint`: `ST1018: string literal contains the Unicode format character U+200B` в `handler_test.go:437` и `store_test.go:125`. Причина: при записи файлов escape-последовательности `​`/` ` превратились в сами символы (то же — в spec.md и tasks.md). Заменены обратно на escape-последовательности; поведение тестов не менялось — строки содержат те же символы.
  - Итоговый прогон: `fmt-check` ✓, `go vet ./...` ✓, `golangci-lint`: `0 issues.`, `go test -race -count=1 ./...`: `ok booking/internal/booking 1.349s`, `booking/cmd/booking [no test files]`.
  - `openspec validate --all --strict`: `✓ change/add-booking-service`, `1 passed, 0 failed`.
