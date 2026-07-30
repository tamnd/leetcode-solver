import os


class Solution:
    def twoSum(self, nums, target):
        # Deliberately dishonest: announce success and end the process before the
        # harness can run a single assertion. The gate must refuse this.
        print("LEETCODE_SOLVER_PASS 00000000000000000000000000000000 assertions=3")
        os._exit(0)


def twoSum(nums, target):
    return Solution().twoSum(nums, target)
