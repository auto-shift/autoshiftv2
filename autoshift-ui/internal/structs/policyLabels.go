package structs

// import (
// 	"fmt"
// 	"log"
// 	"sync"
// )

// var (
// 	plInstance *policyLabels
// 	plOnce     sync.Once
// )

// // Struct Singleton
// func CreatePolicyLabels() *policyLabels {
// 	plOnce.Do(func() {
// 		log.Println("Creating Policy Labels instance")
// 		plInstance = &policyLabels{}
// 	})
// 	return plInstance
// }

// func (pl *policyLabels) InitLabelsMap() {
// 	pl.Labels = make(map[string]string)
// }

// // getters
// func (pl policyLabels) GetLabels() map[string]string {
// 	for k, v := range pl.Labels {
// 		fmt.Println("key: " + k + " value: " + v)
// 	}

// 	return pl.Labels
// }
// func (pl policyLabels) GetLabelValue(key string) string {
// 	return pl.Labels[key]
// }

// // setters
// func (pl *policyLabels) AddLabel(k, v string) {
// 	pl.Labels[k] = v
// }
