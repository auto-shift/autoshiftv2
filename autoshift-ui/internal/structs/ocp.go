package structs

// import (
// 	"log"
// 	"sync"
// )

// type ocpCredStruct struct {
// 	Ocp struct {
// 		User  string `yaml:"user"`
// 		Token string `yaml:"token"`
// 		Url   string `yaml:"url"`
// 	} `yaml:"ocp"`
// }

// var (
// 	ocpInstance *ocpCredStruct
// 	ocp_once    sync.Once
// )

// // Struct Singleton
// func CreateOcpCreds() *ocpCredStruct {
// 	ocp_once.Do(func() {
// 		log.Println("Creating HubValues instance")
// 		ocpInstance = &ocpCredStruct{}
// 	})
// 	return ocpInstance
// }
