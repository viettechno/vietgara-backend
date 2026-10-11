package kafka_test

import "errors"

var (
	errKafkaTransient = errors.New("transient")
	errKafkaPermanent = errors.New("permanent")
	errKafkaDown      = errors.New("down")
)
