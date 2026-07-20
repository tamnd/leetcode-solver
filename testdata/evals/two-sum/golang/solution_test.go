package main

import "testing"

func TestTwoSum(t *testing.T) {
	cases := []struct {
		nums   []int
		target int
		a, b   int
	}{
		{[]int{2, 7, 11, 15}, 9, 0, 1}, {[]int{3, 2, 4}, 6, 1, 2},
		{[]int{3, 3}, 6, 0, 1}, {[]int{-3, 4, 3, 90}, 0, 0, 2},
		{[]int{0, 4, 3, 0}, 0, 0, 3}, {[]int{-1, -2, -3, -4, -5}, -8, 2, 4},
		{[]int{1_000_000_000, -1_000_000_000}, 0, 0, 1},
		{[]int{1, 5, 9, 13, 17, 21}, 38, 4, 5}, {[]int{5, 75, 25}, 100, 1, 2},
		{[]int{2, 5, 5, 11}, 10, 1, 2},
	}
	for _, tc := range cases {
		got := twoSum(tc.nums, tc.target)
		if len(got) != 2 || got[0] == got[1] || tc.nums[got[0]]+tc.nums[got[1]] != tc.target {
			t.Fatalf("twoSum(%v,%d)=%v", tc.nums, tc.target, got)
		}
		if !((got[0] == tc.a && got[1] == tc.b) || (got[0] == tc.b && got[1] == tc.a)) {
			t.Fatalf("indices=%v", got)
		}
	}
}
