import importlib.util
spec = importlib.util.spec_from_file_location("solution", "/workspace/solution.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
cases = [([2,7,11,15],9,{0,1}),([3,2,4],6,{1,2}),([3,3],6,{0,1})]
for nums,target,expected in cases:
    answer=module.Solution().twoSum(nums,target)
    assert set(answer)==expected and nums[answer[0]]+nums[answer[1]]==target
print("PASS 3 tests")
