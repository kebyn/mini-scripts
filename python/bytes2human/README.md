# bytes2human (Python)

将字节大小整数转换为人类可读的单位字符串（`B`、`KB`、`MB`、`GB`、`TB`、`PB`）。

## 使用示例

```bash
# 命令行直接转换
python3 bytes2human.py 1048576 10737418240
# 输出:
# 1024KB
# 10GB
```

或在 Python 代码中导入使用：

```python
from bytes2human import bytes2human

print(bytes2human(104857600))  # 100MB
```
