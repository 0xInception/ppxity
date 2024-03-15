package perplexity

import (
	"fmt"
	"log"
	"testing"
	"time"
)

func TestChatClient_Start(t *testing.T) {
	ppxity := NewChatClient(true, false)
	err := ppxity.Connect()
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	defer ppxity.Close()
	err = ppxity.SendMessage("Hello, World!", CLAUDE)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	response, err := ppxity.ReceiveMessage(time.Second * 50)
	if err != nil {
		log.Fatalf("Failed to receive message: %v", err)
	}
	fmt.Println("Response:", response)
	err = ppxity.SendMessage("What message did i send?", CLAUDE)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	response, err = ppxity.ReceiveMessage(time.Second * 50)
	if err != nil {
		log.Fatalf("Failed to receive message: %v", err)
	}
	fmt.Println("Response:", response)
	time.Sleep(time.Second * 10)
}
