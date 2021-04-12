package main

import (
	"encoding/json"
	"log"
)

type Response struct {
	data map[string]interface{}
}

func (r *Response) addAttribute(name string, data interface{}) {
	r.data[name] = data
}

func (r *Response) json() []byte {
	j, err := json.Marshal(r.data)
	if err != nil {
		log.Println(err.Error())
		//log.Fatal(err.Error())
		return []byte{}
	}
	return j
}

func NewResponse() Response {
	return Response{make(map[string]interface{})}
}

func ErrResponse(err error) interface{} {
	resp := NewResponse()
	resp.addAttribute("error", err.Error())
	return resp.json()
}

func ResultOrErrResponse(data interface{}, err error) interface{}{
	if err != nil {
		return ErrResponse(err)
	}
	return data
}
