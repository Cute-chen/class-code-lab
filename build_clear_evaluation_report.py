#!/usr/bin/env python3
import csv
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path


def read_csv(path):
    with path.open(encoding="utf-8-sig", newline="") as handle:
        return list(csv.DictReader(handle))


def excerpt(text, limit=80):
    text = re.sub(r"\s+", " ", text or "").strip().replace("|", "／").replace("`", "'")
    return text[:limit] + ("…" if len(text) > limit else "")


def is_specific_followup(text):
    text = re.sub(r"\s+", "", text or "")
    if len(text) < 8:
        return False
    boilerplate = ("我想做一个愤怒的小鸟", "我想做一个太空飞船", "我想做一个像素小屋", "请检查当前代码", "请在不改变核心玩法")
    return not text.startswith(boilerplate)


def int_value(row, key):
    try:
        return int(float(row.get(key) or 0))
    except ValueError:
        return 0


def score_goal(prompts, avg_chars):
    if prompts == 0:
        return 0
    if prompts >= 10 and avg_chars >= 50:
        return 4
    if prompts >= 6 or avg_chars >= 50:
        return 3
    if prompts >= 3:
        return 2
    return 1


def score_iteration(revisions):
    return 4 if revisions >= 10 else 3 if revisions >= 6 else 2 if revisions >= 3 else 1 if revisions >= 1 else 0


def score_adoption(proposals, applied):
    if proposals == 0:
        return 0
    if applied >= 8:
        return 4
    if applied >= 4:
        return 3
    if applied >= 1:
        return 2
    return 1


def score_completion(published, published_chars, draft_chars):
    if published and published_chars >= 500:
        return 4
    if published:
        return 3
    if draft_chars >= 500:
        return 2
    if draft_chars > 0:
        return 1
    return 0


def score_debug(messages, debug_messages):
    if not messages:
        return 0
    if debug_messages >= 4:
        return 4
    if debug_messages >= 2:
        return 3
    if debug_messages == 1:
        return 2
    return 1


def level(total):
    if total >= 17:
        return "表现突出"
    if total >= 13:
        return "表现良好"
    if total >= 9:
        return "正在形成"
    return "需要重点指导"


def next_step(row):
    actions = []
    if row["goal_score"] <= 2:
        actions.append("把目标、功能要求和完成标准写清楚")
    if row["iteration_score"] <= 2:
        actions.append("完成一次‘测试—记录问题—修改—再测试’循环")
    if row["adoption_score"] <= 1:
        actions.append("应用 AI 提案前先说明准备解决的问题，并选择性应用")
    if row["completion_score"] <= 2:
        actions.append("完成一个可运行版本并发布")
    if not actions:
        actions.append("口头讲解关键代码，并完成一次不依赖 AI 的小修改")
    return "；".join(actions) + "。"


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: build_clear_evaluation_report.py <export-dir>")
    out = Path(sys.argv[1])
    analysis = read_csv(out / "student_analysis.csv")
    conversations = read_csv(out / "ai_conversations.csv")
    messages = read_csv(out / "ai_messages.csv")

    conversation_user = {int(row["id"]): int(row["user_id"]) for row in conversations}
    prompts_by_user = defaultdict(list)
    for message in messages:
        if message.get("role") == "user":
            user_id = conversation_user.get(int(message["conversation_id"]))
            if user_id is not None:
                prompts_by_user[user_id].append(message.get("content") or "")
    debug_terms = ("报错", "错误", "bug", "调试", "修复", "为什么", "不显示", "无法")
    debug_by_user = Counter()
    for message in messages:
        if message.get("role") != "user":
            continue
        content = message.get("content") or ""
        if any(term in content.lower() for term in debug_terms):
            user_id = conversation_user.get(int(message["conversation_id"]))
            if user_id is not None:
                debug_by_user[user_id] += 1

    rows = []
    for item in analysis:
        student_id = int(item["student_id"])
        prompts = int_value(item, "user_message_count")
        avg_chars = float(item.get("avg_user_prompt_chars") or 0)
        proposal_count = int_value(item, "proposal_count")
        applied = int_value(item, "applied_proposal_count")
        published = item.get("published_work") == "True"
        student_prompts = prompts_by_user[student_id]
        followups = [text for text in student_prompts[1:] if is_specific_followup(text)]
        evidence = "；".join([f"首次：{excerpt(student_prompts[0])}" if student_prompts else "无提问记录", f"后续：{excerpt(followups[0])}" if followups else "没有筛出具体后续修改要求"])
        row = {
            "student_id": student_id,
            "name": item["name"],
            "goal_score": score_goal(prompts, avg_chars),
            "iteration_score": score_iteration(int_value(item, "revision_count")),
            "adoption_score": score_adoption(proposal_count, applied),
            "completion_score": score_completion(published, int_value(item, "published_code_chars"), int_value(item, "draft_code_chars")),
            "debug_score": score_debug(prompts, debug_by_user[student_id]),
            "conversation_count": int_value(item, "conversation_count"),
            "user_message_count": prompts,
            "avg_prompt_chars": avg_chars,
            "proposal_count": proposal_count,
            "applied_proposal_count": applied,
            "apply_rate": round(applied / proposal_count * 100, 1) if proposal_count else 0,
            "revision_count": int_value(item, "revision_count"),
            "debug_message_count": debug_by_user[student_id],
            "published_work": "是" if published else "否",
            "draft_code_chars": int_value(item, "draft_code_chars"),
            "published_code_chars": int_value(item, "published_code_chars"),
            "evidence": evidence,
        }
        row["total_score"] = sum(row[key] for key in ("goal_score", "iteration_score", "adoption_score", "completion_score", "debug_score"))
        row["level"] = level(row["total_score"])
        row["performance"] = (
            f"需求表达{row['goal_score']}/4；迭代调试{row['iteration_score']}/4；"
            f"AI建议落地{row['adoption_score']}/4（{applied}/{proposal_count}，采纳率{row['apply_rate']}%）；"
            f"作品完成度{row['completion_score']}/4；调试表达{row['debug_score']}/4。"
        )
        row["next_step"] = next_step(row)
        rows.append(row)

    rows.sort(key=lambda row: (-row["total_score"], -row["completion_score"], -row["iteration_score"], row["name"]))
    counts = Counter(row["level"] for row in rows)
    report = [
        "# 高一1班课堂评价标准与学生表现",
        "",
        "## 一、这份报告评价什么",
        "",
        "这份报告只评价系统中可以直接观察到的学习过程：学生怎样描述目标、怎样迭代和调试、怎样使用 AI 提案、作品是否完成，以及是否留下调试记录。它不直接评价学生的独立编程能力、课堂专注度、口头讲解能力或作品审美；这些内容需要教师现场观察。",
        "",
        "## 二、‘采纳建议’到底是什么意思",
        "",
        "AI 生成代码后，系统会保存一条‘代码提案’。学生点击‘应用’后，这条提案的 `applied_at` 字段会被写入时间。报告中的‘采纳建议数’就是 `applied_at` 不为空的提案数量。",
        "",
        "它不表示：",
        "",
        "- AI 建议一定正确；",
        "- 学生完整保留了 AI 生成的代码；",
        "- 学生一定理解了这段代码；",
        "- 采纳次数越多，能力就一定越强。",
        "",
        "报告同时列出‘提案总数’和‘采纳率’，方便区分‘没有生成提案’、‘生成了但没有应用’和‘多次选择性应用’。课堂上应要求学生解释为什么应用或修改某个提案。",
        "",
        "## 三、课堂可直接使用的评分标准",
        "",
        "总分20分，每项0–4分。阈值固定，教师可以在课堂记录表中直接使用。",
        "",
        "| 维度 | 4分 | 3分 | 2分 | 1分 | 0分 |",
        "|---|---|---|---|---|---|",
        "| 需求表达 | 至少10次提问，且平均每次不少于50字 | 至少6次提问，或平均每次不少于50字 | 3–5次提问 | 1–2次提问 | 没有提问 |",
        "| 迭代调试 | 10次及以上作品修订 | 6–9次修订 | 3–5次修订 | 1–2次修订 | 没有修订 |",
        "| AI建议落地 | 应用8条及以上提案 | 应用4–7条提案 | 应用1–3条提案 | 有提案但应用0条 | 没有提案 |",
        "| 作品完成度 | 已发布，且发布代码超过500字符 | 已发布，但代码不超过500字符 | 未发布但草稿超过500字符 | 有少量草稿代码 | 没有代码 |",
        "| 调试表达 | 至少4条提问明确描述报错、无法运行、修改原因等问题 | 2–3条 | 1条 | 有提问但没有明显调试描述 | 没有提问 |",
        "",
        "### 总分解释",
        "",
        "- 17–20分：表现突出。已经形成‘明确目标—尝试—调试—修改—完成’的完整过程。",
        "- 13–16分：表现良好。能够完成主要任务，但某个环节还不稳定。",
        "- 9–12分：正在形成。已经开始使用工具，但过程证据不完整。",
        "- 0–8分：需要重点指导。建议教师安排一次小目标、分步骤、有明确验收标准的练习。",
        "",
        "## 四、班级整体情况",
        "",
        f"本次导出包含54名学生、54份作品、107个AI对话、1013条AI消息、342条AI提案和531条AI用量记录。按上述标准统计：表现突出 {counts['表现突出']} 人，表现良好 {counts['表现良好']} 人，正在形成 {counts['正在形成']} 人，需要重点指导 {counts['需要重点指导']} 人。",
        "",
        "## 五、学生逐人表现",
        "",
        "| 学生 | 总分 | 等级 | 需求 | 迭代 | 落地 | 完成 | 调试 | 提案应用 | 作品 | 关键证据 | 下一步 |",
        "|---|---:|---|---:|---:|---:|---:|---:|---|---|---|---|",
    ]
    for row in rows:
        report.append(
            f"| {row['name']} | {row['total_score']}/20 | {row['level']} | {row['goal_score']} | {row['iteration_score']} | {row['adoption_score']} | {row['completion_score']} | {row['debug_score']} | {row['applied_proposal_count']}/{row['proposal_count']}（{row['apply_rate']}%） | {row['published_work']} | {row['evidence']} | {row['next_step']} |"
        )
    report += [
        "",
        "## 六、教师使用建议",
        "",
        "1. 先看分项分数，不要只看总分。比如‘作品完成度4分、需求表达1分’说明作品做出来了，但需求描述过程较弱。",
        "2. 课堂追问可以使用：‘你让 AI 修改了什么？为什么要应用这个提案？如果不用 AI，你能指出这段代码的哪一部分？’",
        "3. 对‘需要重点指导’的学生，下一次任务应把目标拆成可验收的小步骤，并要求至少完成一次测试、记录问题和修改。",
        "4. 对‘表现突出’的学生，可以增加解释代码、同伴讲解和不依赖 AI 的小改动，确认其理解程度。",
        "",
        "数据来源：高一1班导出目录中的 CSV、JSON 和作品源代码。统计结果反映导出时点，不替代教师的课堂观察。",
    ]
    target = out / "课堂评价标准与学生表现_明确版.md"
    target.write_text("\n".join(report) + "\n", encoding="utf-8")
    print(target)


if __name__ == "__main__":
    main()
