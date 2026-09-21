package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func createScoringStudent(t *testing.T, a *App, class *Class, name string) User {
	t.Helper()
	student := User{ClassID: &class.ID, Class: class, Role: RoleStudent, Name: name, LoginName: fmt.Sprintf("%s-%d", name, time.Now().UnixNano()), PasswordHash: "x"}
	if err := a.DB.Create(&student).Error; err != nil {
		t.Fatal(err)
	}
	return student
}

func createPublishedWork(t *testing.T, a *App, class Class, owner User, title string, publishedAt time.Time) Work {
	t.Helper()
	work := Work{UserID: owner.ID, ClassID: class.ID, Title: title, DraftCode: "<html><body>ok</body></html>", PublishedCode: "<html><body>ok</body></html>", PublishedVersion: 1, IsPublished: true, PublishedAt: &publishedAt}
	if err := a.DB.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	return work
}

func scoreWork(a *App, voter User, workID uint, points int) (int, []byte) {
	c, recorder := testContext(http.MethodPut, fmt.Sprintf("/api/gallery/%d/score", workID), fmt.Sprintf(`{"points":%d}`, points))
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(workID)}}
	c.Set("auth", authContext{User: voter})
	a.handleScoreWork(c)
	return recorder.Code, recorder.Body.Bytes()
}

func TestWorkScoringLifecycleAndEligibility(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "评分班", Active: true, ScoreBudget: 10}
	a.DB.Create(&class)
	voter := createScoringStudent(t, a, &class, "评分者")
	studentB := createScoringStudent(t, a, &class, "作品乙")
	studentC := createScoringStudent(t, a, &class, "作品丙")
	now := time.Now()
	selfWork := createPublishedWork(t, a, class, voter, "自己的作品", now.Add(-3*time.Hour))
	workB := createPublishedWork(t, a, class, studentB, "作品 B", now.Add(-2*time.Hour))
	workC := createPublishedWork(t, a, class, studentC, "作品 C", now.Add(-time.Hour))

	code, body := scoreWork(a, voter, workB.ID, 6)
	if code != http.StatusOK {
		t.Fatalf("score B failed: code=%d body=%s", code, body)
	}
	var response struct {
		Scoring scoringSummary `json:"scoring"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Scoring.Spent != 6 || response.Scoring.Remaining != 4 || response.Scoring.Completed {
		t.Fatalf("unexpected scoring summary: %#v", response.Scoring)
	}

	if code, _ = scoreWork(a, voter, workC.ID, 5); code != http.StatusBadRequest {
		t.Fatalf("expected budget rejection, got %d", code)
	}
	if code, body = scoreWork(a, voter, workC.ID, 4); code != http.StatusOK {
		t.Fatalf("score C failed: code=%d body=%s", code, body)
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Scoring.Spent != 10 || response.Scoring.Remaining != 0 || !response.Scoring.Completed {
		t.Fatalf("expected completed scoring: %#v", response.Scoring)
	}
	if code, _ = scoreWork(a, voter, workB.ID, 3); code != http.StatusOK {
		t.Fatalf("adjust score failed: %d", code)
	}
	var adjusted []WorkScore
	a.DB.Where("voter_id = ? AND work_id = ?", voter.ID, workB.ID).Find(&adjusted)
	if len(adjusted) != 1 || adjusted[0].Points != 3 {
		t.Fatalf("score adjustment should update one row: %#v", adjusted)
	}
	if code, _ = scoreWork(a, voter, workB.ID, 0); code != http.StatusBadRequest {
		t.Fatalf("expected score withdrawal rejection, got %d", code)
	}
	if code, body = scoreWork(a, voter, workB.ID, 3); code != http.StatusOK {
		t.Fatalf("score resubmission failed: code=%d body=%s", code, body)
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Scoring.Spent != 7 || response.Scoring.Remaining != 3 {
		t.Fatalf("unexpected summary after score resubmission: %#v", response.Scoring)
	}
	if code, _ = scoreWork(a, voter, selfWork.ID, 1); code != http.StatusBadRequest {
		t.Fatalf("expected self-score rejection, got %d", code)
	}

	otherClass := Class{Name: "其他班", Active: true, ScoreBudget: 10}
	a.DB.Create(&otherClass)
	otherOwner := createScoringStudent(t, a, &otherClass, "其他班作者")
	otherWork := createPublishedWork(t, a, otherClass, otherOwner, "跨班作品", now)
	if code, _ = scoreWork(a, voter, otherWork.ID, 1); code != http.StatusForbidden {
		t.Fatalf("expected cross-class rejection, got %d", code)
	}
	a.DB.Model(&workB).Update("is_locked", true)
	if code, _ = scoreWork(a, voter, workB.ID, 1); code != http.StatusBadRequest {
		t.Fatalf("expected locked-work rejection, got %d", code)
	}
	a.DB.Model(&workB).Updates(map[string]any{"is_locked": false, "is_published": false})
	if code, _ = scoreWork(a, voter, workB.ID, 1); code != http.StatusBadRequest {
		t.Fatalf("expected unpublished-work rejection, got %d", code)
	}
}

func TestGalleryRankingUsesSupportersThenPublishedTime(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "排名班", Active: true, ScoreBudget: 20}
	a.DB.Create(&class)
	authorA := createScoringStudent(t, a, &class, "作者甲")
	authorB := createScoringStudent(t, a, &class, "作者乙")
	authorC := createScoringStudent(t, a, &class, "作者丙")
	authorD := createScoringStudent(t, a, &class, "作者丁")
	voterA := createScoringStudent(t, a, &class, "投票甲")
	voterB := createScoringStudent(t, a, &class, "投票乙")
	now := time.Now()
	workOneSupporter := createPublishedWork(t, a, class, authorA, "单人支持", now.Add(-4*time.Hour))
	workLater := createPublishedWork(t, a, class, authorB, "较晚发布", now.Add(-2*time.Hour))
	workEarlier := createPublishedWork(t, a, class, authorC, "较早发布", now.Add(-3*time.Hour))
	workZero := createPublishedWork(t, a, class, authorD, "零分作品", now.Add(-time.Hour))
	scores := []WorkScore{
		{ClassID: class.ID, VoterID: voterA.ID, WorkID: workOneSupporter.ID, Points: 10},
		{ClassID: class.ID, VoterID: voterA.ID, WorkID: workLater.ID, Points: 5},
		{ClassID: class.ID, VoterID: voterB.ID, WorkID: workLater.ID, Points: 5},
		{ClassID: class.ID, VoterID: voterA.ID, WorkID: workEarlier.ID, Points: 6},
		{ClassID: class.ID, VoterID: voterB.ID, WorkID: workEarlier.ID, Points: 4},
	}
	if err := a.DB.Create(&scores).Error; err != nil {
		t.Fatal(err)
	}

	items, err := a.listGallery(class.ID, false, authorA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected four works, got %d", len(items))
	}
	if items[0].ID != workEarlier.ID || items[0].Rank != 1 || items[1].ID != workLater.ID || items[1].Rank != 2 || items[2].ID != workOneSupporter.ID || items[2].Rank != 3 {
		t.Fatalf("unexpected ranking order: %#v", items)
	}
	if items[3].ID != workZero.ID || items[3].Rank != 0 {
		t.Fatalf("zero-score work should be unranked: %#v", items[3])
	}
}

func TestScoreBudgetCanPauseButNotDropBelowSpent(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "额度班", Active: true, ScoreBudget: 10}
	a.DB.Create(&class)
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "score-budget-teacher", PasswordHash: "x"}
	a.DB.Create(&teacher)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	voter := createScoringStudent(t, a, &class, "评分学生")
	owner := createScoringStudent(t, a, &class, "作品学生")
	work := createPublishedWork(t, a, class, owner, "额度作品", time.Now())
	a.DB.Create(&WorkScore{ClassID: class.ID, VoterID: voter.ID, WorkID: work.ID, Points: 8})

	updateBudget := func(value int) (int, string) {
		c, recorder := testContext(http.MethodPatch, fmt.Sprintf("/api/teacher/classes/%d", class.ID), fmt.Sprintf(`{"score_budget":%d}`, value))
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(class.ID)}}
		c.Set("auth", authContext{User: teacher})
		a.handleUpdateClass(c)
		return recorder.Code, recorder.Body.String()
	}
	if code, _ := updateBudget(7); code != http.StatusBadRequest {
		t.Fatalf("expected lower-budget rejection, got %d", code)
	}
	if code, body := updateBudget(0); code != http.StatusOK {
		t.Fatalf("pause scoring failed: code=%d body=%s", code, body)
	}
	a.DB.First(&class, class.ID)
	if class.ScoreBudget != 0 {
		t.Fatalf("expected paused budget, got %d", class.ScoreBudget)
	}
	var count int64
	a.DB.Model(&WorkScore{}).Where("class_id = ?", class.ID).Count(&count)
	if count != 1 {
		t.Fatalf("pausing should preserve scores, count=%d", count)
	}
	if code, _ := scoreWork(a, voter, work.ID, 7); code != http.StatusForbidden {
		t.Fatalf("expected scoring to be disabled, got %d", code)
	}
	if code, body := updateBudget(8); code != http.StatusOK {
		t.Fatalf("resume scoring failed: code=%d body=%s", code, body)
	}
}

func TestUnpublishAndLockReturnAllocatedScores(t *testing.T) {
	a := newTestApp(t)
	class := Class{Name: "退分班", Active: true, PublishEnabled: true, ScoreBudget: 10}
	a.DB.Create(&class)
	owner := createScoringStudent(t, a, &class, "作品作者")
	voter := createScoringStudent(t, a, &class, "投票学生")
	work := createPublishedWork(t, a, class, owner, "退分作品", time.Now())
	a.DB.Create(&WorkScore{ClassID: class.ID, VoterID: voter.ID, WorkID: work.ID, Points: 6})

	c, recorder := testContext(http.MethodPost, "/api/student/work/publish", "")
	c.Set("auth", authContext{User: owner})
	a.handlePublishWork(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("republish failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var count int64
	a.DB.Model(&WorkScore{}).Where("work_id = ?", work.ID).Count(&count)
	if count != 1 {
		t.Fatal("publishing a new version should preserve scores")
	}

	c, recorder = testContext(http.MethodPost, "/api/student/work/unpublish", "")
	c.Set("auth", authContext{User: owner})
	a.handleUnpublishOwnWork(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("student unpublish failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.Model(&WorkScore{}).Where("work_id = ?", work.ID).Count(&count)
	if count != 0 {
		t.Fatalf("unpublish should clear scores, count=%d", count)
	}

	a.DB.Model(&work).Updates(map[string]any{"is_published": true, "is_locked": false})
	a.DB.Create(&WorkScore{ClassID: class.ID, VoterID: voter.ID, WorkID: work.ID, Points: 4})
	teacher := User{Role: RoleTeacher, Name: "教师", LoginName: "score-lock-teacher", PasswordHash: "x"}
	a.DB.Create(&teacher)
	a.DB.Create(&TeacherClassAccess{TeacherID: teacher.ID, ClassID: class.ID})
	c, recorder = testContext(http.MethodPost, fmt.Sprintf("/api/teacher/works/%d/lock", work.ID), "")
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(work.ID)}, {Key: "action", Value: "lock"}}
	c.Set("auth", authContext{User: teacher})
	a.handleTeacherWorkAction(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("teacher lock failed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	a.DB.Model(&WorkScore{}).Where("work_id = ?", work.ID).Count(&count)
	if count != 0 {
		t.Fatalf("locking should clear scores, count=%d", count)
	}
}
