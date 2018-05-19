package leveldb

import (
	"encoding/json"
	"errors"
	"sync"

	"reflect"

	"fmt"

	"strings"

	"github.com/it-chain/eventsource"
	"github.com/it-chain/eventsource/store"
	"github.com/it-chain/leveldb-wrapper"
)

var ErrNilEvents = errors.New("no event history exist")
var ErrGetValue = errors.New("fail to get value from leveldb")

type SerializedEvent struct {
	Type string
	Data []byte
}

type History []SerializedEvent

//Leveldb store implementing store interface
type Store struct {
	mux        *sync.RWMutex
	db         *leveldbwrapper.DB
	serializer EventSerializer
}

func NewEventStore(path string, serializer EventSerializer) store.EventStore {

	db := leveldbwrapper.CreateNewDB(path)
	db.Open()

	return &Store{
		db:         db,
		mux:        &sync.RWMutex{},
		serializer: serializer,
	}
}

//Save Events to leveldb(key is aggregateID)
func (s Store) Save(aggregateID string, events ...eventsource.Event) error {

	s.mux.Lock()
	defer s.mux.Unlock()

	history, err := s.getHistory(aggregateID)

	if err != nil {
		return err
	}

	//new history
	if history == nil {
		history = &History{}
	}

	for _, event := range events {
		serializedEvent, err := s.serializer.Marshal(event)

		if err != nil {
			return err
		}

		*history = append(*history, serializedEvent)
	}

	historyValue, err := json.Marshal(history)

	if err != nil {
		return err
	}

	return s.db.Put([]byte(aggregateID), historyValue, true)
}

//Load Aggregate Event from leveldb
func (s Store) Load(aggregateID string) ([]eventsource.Event, error) {

	history, err := s.getHistory(aggregateID)

	if err != nil {
		return nil, err
	}

	//new history
	if history == nil {
		return nil, ErrNilEvents
	}

	events := make([]eventsource.Event, 0)

	for _, value := range *history {
		event, err := s.serializer.Unmarshal(value)

		if err != nil {
			return []eventsource.Event{}, err
		}

		events = append(events, event)
	}

	return events, nil
}

func (s Store) getHistory(aggregateID string) (*History, error) {

	var history = &History{}

	historyValue, err := s.db.Get([]byte(aggregateID))

	if err != nil {
		return nil, ErrGetValue
	}

	//history does not exist
	if historyValue == nil {
		return nil, nil
	}

	err = json.Unmarshal(historyValue, history)

	if err != nil {
		return nil, err
	}

	return history, nil
}

type EventSerializer interface {
	// MarshalEvent converts an Event to a Record
	Marshal(event eventsource.Event) (SerializedEvent, error)

	// UnmarshalEvent converts an Event backed into a Record
	Unmarshal(serializedEvent SerializedEvent) (eventsource.Event, error)
}

type JSONSerializer struct {
	eventTypes map[string]reflect.Type
}

func NewSerializer(events ...eventsource.Event) EventSerializer {
	s := &JSONSerializer{
		eventTypes: make(map[string]reflect.Type),
	}

	s.Register(events...)

	return s
}

func (j *JSONSerializer) Register(events ...eventsource.Event) {

	for _, event := range events {
		rawType, name := GetTypeName(event)
		j.eventTypes[name] = rawType
	}
}

func (j *JSONSerializer) Marshal(e eventsource.Event) (SerializedEvent, error) {

	serializedEvent := SerializedEvent{}
	_, name := GetTypeName(e)
	serializedEvent.Type = name

	data, err := json.Marshal(e)

	if err != nil {
		return SerializedEvent{}, err
	}

	serializedEvent.Data = data

	return serializedEvent, nil
}

func (j *JSONSerializer) Unmarshal(serializedEvent SerializedEvent) (eventsource.Event, error) {

	t, ok := j.eventTypes[serializedEvent.Type]

	if !ok {
		return nil, errors.New(fmt.Sprintf("unbound event type, %v", serializedEvent.Type))
	}

	v := reflect.New(t).Interface()

	err := json.Unmarshal(serializedEvent.Data, v)
	if err != nil {
		return nil, err
	}

	return v.(eventsource.Event), nil
}

func GetTypeName(source interface{}) (reflect.Type, string) {

	rawType := reflect.TypeOf(source)

	if rawType.Kind() == reflect.Ptr {
		rawType = rawType.Elem()
	}

	name := rawType.String()
	parts := strings.Split(name, ".")
	return rawType, parts[1]
}
