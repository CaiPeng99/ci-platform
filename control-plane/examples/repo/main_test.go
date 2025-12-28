package main

import "testing"

func TestAdd(t *testing.T){
	result := Add(2,3)
	if result != 5 {
		t.Errorf("Add(2,3) = %d; expected to be 5", result)
	}

}

func TestMultiply(t *testing.T){
	result := Multiply(4,5)
	if result != 20 {
		t.Errorf("Multiply(=4,5) = %d; expected to be 20", result)
	}
	
}