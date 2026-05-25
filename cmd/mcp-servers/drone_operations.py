#!/usr/bin/env python3
import json, sys
SKILL_DEF={"name":"drone_operations","description":"航线规划、任务策略、安全评估","tools":[{"name":"plan_mission","description":"航线规划、任务策略、安全评估","inputSchema":{"type":"object","properties":{"objective": {"type": "string", "description": "objective"}},"required":["objective"]}}]}
HANDLERS={}
def tool(f): HANDLERS[f.__name__]=f; return f

@tool
def plan_mission(args):
    objective=args.get("objective","")
    return f"# 无人机任务\n\n## 分析结果\n根据无人机任务技能分析输入。\n\n---\n*Generated*"

def handle(req):
    m=req.get("method",""); r=req.get("id",""); p=req.get("params",{}) or {}
    if m=="initialize": return {"jsonrpc":"2.0","id":r,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":SKILL_DEF["name"],"version":"1.0"}}}
    if m=="tools/list": return {"jsonrpc":"2.0","id":r,"result":{"tools":SKILL_DEF["tools"]}}
    if m=="tools/call":
        n=p.get("name",""); a=p.get("arguments",{})
        h=HANDLERS.get(n)
        if h:
            try: return {"jsonrpc":"2.0","id":r,"result":{"content":[{"type":"text","text":h(a)}]}}
            except Exception as e: return {"jsonrpc":"2.0","id":r,"result":{"content":[{"type":"text","text":str(e)}],"isError":True}}
        else: return {"jsonrpc":"2.0","id":r,"result":{"content":[{"type":"text","text":f"unknown tool: {n}"}],"isError":True}}
    if m=="shutdown": sys.exit(0)
    return {"jsonrpc":"2.0","id":r,"error":{"message":f"unknown method: {m}"}}
def main():
    for l in sys.stdin:
        l=l.strip()
        if not l: continue
        try:
            r=handle(json.loads(l))
            sys.stdout.write(json.dumps(r,ensure_ascii=False)+"\n"); sys.stdout.flush()
        except: pass
if __name__=="__main__": main()
