package recorder

import "fmt"

type Participant struct {

}

type Room struct {
	id string
	name string
}

func NewRoom(inId string, inName string) (*Room, error) {
	r := Room{}
	r.id = inId
	r.name = inName
	fmt.Printf("Created room id = %s, name = %s\n", r.id, r.name)
	return &r, nil
}

func (r *Room) GetId() string {
	return r.id
}

func (r *Room) GetName() string {
	return r.name
}
