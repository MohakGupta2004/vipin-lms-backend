# Manual testing guide: admin course ownership, new read endpoints, edit/delete

This guide covers every endpoint added or changed for the LMS frontend. Run the steps in order. Each step uses IDs saved by earlier steps.

- Base URL: `http://localhost:$PORT/api/v1` (use the `PORT` from `.env`)
- Auth: HttpOnly cookies `access_token` and `refresh_token`. The curl examples keep one cookie jar per user (`-b/-c admin.jar`).
- Every response looks like `{"status":"success","data":...}` or `{"status":"error","message":"..."}`.
- Swagger UI: `http://localhost:$PORT/swagger/index.html` (regenerate with `make swagger`).

## The ownership rule

A user can **manage** a course (lessons, notes, quizzes, posts, status, edit, delete) only when:

1. their role is `instructor` or `admin`, **and**
2. their user ID is the course's `instructorId`.

Being an admin does not let you manage someone else's course. Admins can still **read** any course with `GET /courses/{id}`, list all courses, list users, and manage enrollments, as before.

When you are not the owner, you get the same response as for a missing resource. Lessons, quizzes, notes, posts and course status return `404`, except `POST /courses/{id}/lessons` and `POST /posts`, which return `403`.

---

## 0. Setup

```bash
make migrate-up        # applies all migrations, up to 30_notes_is_free
make run               # or: make air

export B=http://localhost:8080/api/v1   # use your PORT
```

Create four users, then promote them in SQL (there is no promote endpoint):

```bash
for u in admin admin2 inst student; do
  curl -s -c $u.jar -H 'Content-Type: application/json' -X POST $B/auth/register \
    -d "{\"firstName\":\"$u\",\"lastName\":\"Test\",\"email\":\"$u@test.io\",\"password\":\"pw\"}"
done
```

```sql
UPDATE users SET role = 'admin'      WHERE email IN ('admin@test.io', 'admin2@test.io');
UPDATE users SET role = 'instructor' WHERE email = 'inst@test.io';
SELECT id FROM exams LIMIT 1;   -- save as EXAM
```

Roles are read from the database on every request, so you do not need to log in again after changing a role.

```bash
J='-H Content-Type:application/json'
export EXAM=<exam id>
```

---

## 1. `GET /auth/me` (new)

Returns the logged-in user. The frontend calls it on app load.

| # | Request | Expect |
|---|---------|--------|
| 1.1 | `curl -s -b admin.jar $B/auth/me` | `200`, `data.role = "admin"`, fields `id, firstName, lastName, email, role, lastLoginAt`, **no password** |
| 1.2 | `curl -s $B/auth/me` (no cookie) | `401` |
| 1.3 | Change a role in SQL, call again | New role shown immediately |

Save the IDs:

```bash
export ADMIN=<admin id>  INST=<inst id>  STU=<student id>
```

---

## 2. `GET /users?role=` (new, admin only)

Used by the instructor picker and the student picker. Returns active, non-deleted users sorted by name.

Query params: `role` (`student` | `instructor` | `admin`; leave it out to get every role), `limit` (default 50, max 200), `offset` (default 0).

| # | Request | Expect |
|---|---------|--------|
| 2.1 | `curl -s -b admin.jar "$B/users?role=instructor"` | `200`, only instructors; each item has `id, firstName, lastName, email, role`, **no password** |
| 2.2 | `curl -s -b admin.jar "$B/users?role=student&limit=1&offset=0"` | `200`, exactly 1 item |
| 2.3 | `curl -s -b admin.jar "$B/users?role=bogus"` | `400` |
| 2.4 | `curl -s -b inst.jar "$B/users"` | `403` |
| 2.5 | `curl -s -b admin.jar "$B/users?limit=0"` | `400` |

---

## 3. `POST /courses` (changed)

`instructorId` is now **optional**. If you leave it out, the admin creating the course becomes its owner. If you send it, it must be an active `instructor` or `admin`.

| # | Request | Expect |
|---|---------|--------|
| 3.1 | `curl -s -b admin.jar $J -X POST $B/courses -d "{\"examId\":\"$EXAM\",\"title\":\"Admin Course\",\"slug\":\"admin-course\"}"` | `201`, `instructorId = $ADMIN`. Save `data.id` as `AC` |
| 3.2 | Same, with `"instructorId":"$INST"`, `"slug":"inst-course"` | `201`, `instructorId = $INST`. Save as `IC` |
| 3.3 | Same, with `"instructorId":"$STU"` | `400` "instructor not found (must be an active instructor or admin)" |
| 3.4 | `curl -s -b inst.jar ...` | `403` (creating courses is still admin only) |

---

## 4. Admin uses instructor endpoints on their own course (changed)

These endpoints used to return `403` for every admin. Now they work for the owner, whether instructor or admin.

| # | Request | Expect |
|---|---------|--------|
| 4.1 | `curl -s -b admin.jar $J -X POST $B/courses/$AC/lessons -d '{"title":"Chapter 1","isPublished":false}'` | `201`. Save as `L1` |
| 4.2 | Same on `$IC` (the instructor's course) | `403` |
| 4.3 | `admin2.jar` on `$AC` | `403` |
| 4.4 | `curl -s -b admin.jar $J -X POST $B/lessons/$L1/quizzes -d '{"title":"Q1","questions":[{"questionText":"2+2?","options":[{"optionText":"4","isCorrect":true},{"optionText":"5"}]}]}'` | `201`. Save as `QZ` |
| 4.5 | Same with `admin2.jar` | `404` |
| 4.6 | `curl -s -b admin.jar $J -X PATCH $B/quizzes/$QZ/status -d '{"status":"draft"}'` | `200` |
| 4.7 | `curl -s -b admin.jar $J -X PATCH $B/courses/$AC/status -d '{"status":"published"}'` | `200` |
| 4.8 | Same with `admin2.jar` | `404` |
| 4.9 | `curl -s -b admin.jar $J -X POST $B/posts -d "{\"courseId\":\"$AC\",\"content\":\"Hello\"}"` | `201`. Save as `P1` |
| 4.10 | Same with `"courseId":"$IC"` | `403` |
| 4.11 | `curl -s -b admin2.jar -X DELETE $B/posts/$P1` | `404` |
| 4.12 | `curl -s -b admin.jar -X DELETE $B/posts/$P1` | `200` |
| 4.13 | `curl -s -b admin.jar -F title=Notes -F file=@some.pdf $B/lessons/$L1/notes` | `201` with GCS enabled (`503` with GCS off). Save as `N1` |
| 4.14 | `curl -s -b admin.jar -X DELETE $B/notes/$N1` | `200`; the PDF is removed from the bucket |

**Behaviour change:** `DELETE /posts/{id}` and `DELETE /notes/{id}` now check **course ownership**, not authorship. The course owner can delete any post or note in their course. Someone who wrote a post but does not own the course can no longer delete it.

---

## 5. `GET /courses/{id}` (new)

Returns `id, examId, instructorId, title, slug, shortDescription, description, status, isFree, createdAt`.

| Who | Can read |
|-----|----------|
| Admin | Any course, any status |
| Owner (instructor or admin) | Their own course, any status |
| Student | Only if the course is `published` **and** they have an enrollment with status `active`/`completed` that has not expired |
| Anyone else | `404` |

| # | Request | Expect |
|---|---------|--------|
| 5.1 | `admin2.jar` → `GET $B/courses/$IC` | `200` |
| 5.2 | `inst.jar` → `GET $B/courses/$IC` (draft) | `200` |
| 5.3 | `inst.jar` → `GET $B/courses/$AC` | `404` |
| 5.4 | `student.jar` → `GET $B/courses/$AC` (not enrolled) | `404` |
| 5.5 | Enroll: `curl -s -b admin.jar $J -X POST $B/enrollments -d "{\"userId\":\"$STU\",\"courseId\":\"$AC\",\"months\":1}"`, then 5.4 again | `200` |
| 5.6 | Set `$AC` to `draft`, then 5.4 again | `404` (set it back to `published` afterwards) |
| 5.7 | SQL: `UPDATE enrollments SET expires_at = now() - interval '1 day' WHERE user_id = '<STU>'`, then 5.4 again | `404` |
| 5.8 | SQL: set `status = 'cancelled'` (with `expires_at` in the future), then 5.4 again | `404` (restore `status='active'` and the future expiry afterwards) |
| 5.9 | `GET $B/courses/not-a-uuid` | `404` |

---

## 6. `GET /me/courses` (new)

`limit` (default 20, max 100) and `offset` (default 0). Newest first.

| Role | Returns |
|------|---------|
| Student | Published, non-deleted courses with an enrollment that is `active`/`completed` and not expired |
| Instructor / Admin | Courses where `instructorId` = me, **drafts included** |

| # | Request | Expect |
|---|---------|--------|
| 6.1 | `student.jar` | Only `$AC` |
| 6.2 | `inst.jar` | Only `$IC` (draft) |
| 6.3 | `admin.jar` | Only `$AC` (not `$IC`) |
| 6.4 | `admin.jar` with `?limit=0` | `400` |

---

## 7. `PATCH /courses/{id}` (new, owner only)

Only the fields you send are changed: `examId, title, slug, shortDescription, description, isFree`. Status still uses `PATCH /courses/{id}/status`.

| # | Body | Expect |
|---|------|--------|
| 7.1 | `{"title":"Renamed","isFree":true}` on `$AC` | `200`, title and isFree changed, slug unchanged |
| 7.2 | `{"slug":"inst-course"}` | `409` slug taken |
| 7.3 | `{"slug":"Bad Slug"}` | `400` |
| 7.4 | `{"examId":"00000000-0000-0000-0000-000000000000"}` | `400` exam not found |
| 7.5 | Any body on `$IC` with `admin.jar` | `404` |

## 8. `DELETE /courses/{id}` (new, owner only)

Soft delete: it sets `deleted_at`. **Enrollments are not changed**, but every read skips deleted courses, so students lose access right away.

| # | Request | Expect |
|---|---------|--------|
| 8.1 | `admin2.jar` → `DELETE $B/courses/$AC` | `404` |
| 8.2 | Do this one **last** (after sections 9 and 10). `admin.jar` → `DELETE $B/courses/$AC` | `200` |
| 8.3 | `student.jar` → `GET $B/courses/$AC` and `GET $B/me/courses` | `404` / empty list |
| 8.4 | SQL: `SELECT status FROM enrollments WHERE course_id = '<AC>'` | Still `active` |
| 8.5 | Create a new course with slug `admin-course` | `201`: a deleted course's slug can be reused |

---

## 9. `PATCH /lessons/{id}` and `DELETE /lessons/{id}` (new, owner only)

PATCH changes only the fields you send: `title, content, isFree, isPublished`.

| # | Request | Expect |
|---|---------|--------|
| 9.1 | `PATCH $B/lessons/$L1` `{"isPublished":true}` | `200`, only `isPublished` changed |
| 9.2 | `{"isPublished":false}` | `200`; students no longer see the lesson in `GET /courses/{id}/lessons` |
| 9.3 | `{"title":"   "}` | `400` |
| 9.4 | `admin2.jar` | `404` |

DELETE: the lesson and its quizzes are soft-deleted (quiz attempts are kept). The lesson's notes are deleted and their PDFs are removed from GCS.

| # | Request | Expect |
|---|---------|--------|
| 9.5 | Upload a PDF note to the lesson and create a quiz on it, then `DELETE $B/lessons/$L1` | `200` |
| 9.6 | `GET $B/courses/$AC/lessons` | Lesson gone |
| 9.7 | SQL: `SELECT count(*) FROM notes WHERE lesson_id='<L1>'` | `0`; object gone from bucket |
| 9.8 | SQL: `SELECT deleted_at FROM quizzes WHERE lesson_id='<L1>'` | Not null |
| 9.9 | `DELETE` again | `404` |
| 9.10 | With `GCS_ENABLE=false`, delete a lesson that has PDF notes | `503`; nothing deleted, so no files are orphaned |

---

## 10. `PATCH /quizzes/{id}` and `DELETE /quizzes/{id}` (new, owner only)

PATCH changes only the fields you send: `title, description, timeLimitSec, passPercent, isFree, status, questions`.

- `timeLimitSec: 0` makes the quiz untimed.
- `questions` **replaces all questions**, using the same format as create. This is allowed only while nobody has attempted the quiz. After that it returns `409`, because replacing the questions would delete students' recorded answers.
- The response includes the questions with `isCorrect` and explanations, as the owner sees them.

Recreate a quiz first if you already deleted `L1`.

| # | Request | Expect |
|---|---------|--------|
| 10.1 | `{"title":"Q1 v2","timeLimitSec":60,"status":"published","questions":[{"questionText":"3+3?","options":[{"optionText":"6","isCorrect":true},{"optionText":"7"}]}]}` | `200`, new question, `timeLimitSec: 60` |
| 10.2 | Student: `GET $B/quizzes/$QZ`, then `POST $B/quizzes/$QZ/attempts` with an answer | `201` |
| 10.3 | PATCH with `questions` again | `409` "this quiz already has attempts..." |
| 10.4 | PATCH `{"timeLimitSec":0}` | `200`, `timeLimitSec: null` |
| 10.5 | `admin2.jar` → `DELETE $B/quizzes/$QZ` | `404` |
| 10.6 | `admin.jar` → `DELETE $B/quizzes/$QZ` | `200` |
| 10.7 | `GET $B/quizzes/$QZ` and `GET $B/lessons/$L1/quizzes` | `404` / not listed |
| 10.8 | SQL: `SELECT count(*) FROM quiz_attempts WHERE quiz_id='<QZ>'` | Attempts still there |

---

## 10a. Quiz types: mock tests and practice sets (new)

`type` is `mock_test` (default: optional `timeLimitSec`, `passPercent` defaults to 70, graded with marks) or `practice` (untimed, no marks; `timeLimitSec`/`passPercent` must be omitted). Practice submit grades only the answered questions, so the frontend can check one question at a time.

| # | Request | Expect |
|---|---------|--------|
| 10a.1 | Create a quiz with `"type":"practice"` and questions | `201`, `type: practice`, `timeLimitSec`/`passPercent` null |
| 10a.2 | Same with `"timeLimitSec":60` | `400` |
| 10a.3 | `GET $B/lessons/$L1/quizzes?type=practice` | Only practice quizzes |
| 10a.4 | `GET $B/lessons/$L1/quizzes?type=bogus` | `400` |
| 10a.5 | Student: `POST $B/quizzes/$PQ/attempts` with 1 answer | `201`, one answer with explanation, `score/total/passed` null |
| 10a.6 | Student: same with `"answers":[]` | `400` "answer at least one question" |
| 10a.7 | Student: submit to a mock test | Unchanged graded response |
| 10a.8 | `PATCH $B/quizzes/$PQ {"type":"mock_test"}` after an attempt | `409` |
| 10a.9 | `PATCH` a quiz with no attempts to `{"type":"practice"}` | `200`, time limit and pass percent cleared |
| 10a.10 | Student: `GET $B/quizzes/$PQ/attempts` | Practice history, no marks |

---

## 10b. Free preview for notes and quizzes (new)

A note or quiz is open to users previewing a published paid course only when it is marked free itself. A free lesson does **not** unlock its notes, quizzes or videos: each item's own free flag decides. For videos, check `GET $B/courses/$C/videos` as `P`: a paid video in a free lesson has `locked: true`, and `GET $B/videos/<id>/stream` returns `404`. Paid ones are still listed, with `locked: true`, and cannot be opened. Use a paid, published course with a lesson `L1` and a user `P` who is not enrolled. Repeat 10b.4–10b.5 with `L1` marked free: `NP` must stay locked.

| # | Request | Expect |
|---|---------|--------|
| 10b.1 | Owner: `curl -s -b admin.jar -X POST $B/lessons/$L1/notes -F title=Free -F isFree=true -F file=@a.pdf` | `201`, `isFree: true`. Save as `NF` |
| 10b.2 | Owner: same with `-F title=Paid` and no `isFree` | `201`, `isFree: false`. Save as `NP` |
| 10b.3 | Owner: same with `-F isFree=maybe` | `400` |
| 10b.4 | `P`: `GET $B/courses/$C/notes?lessonId=$L1` | Both notes; `NP` has `locked: true`, `NF` has no `locked` |
| 10b.5 | `P`: `GET $B/notes/$NF/file` / `GET $B/notes/$NP/file` | `200` PDF / `404` |
| 10b.6 | `P`: `GET $B/courses/$C/lessons` | `L1` is `locked`, its `notes` list both, `NP` locked |
| 10b.7 | Owner: `PATCH $B/notes/$NP {"isFree":true}` | `200`, `isFree: true`; `P` can now download it |
| 10b.8 | Non-owner instructor: `PATCH $B/notes/$NP {"isFree":false}` | `404` |
| 10b.8a | Owner: `PATCH $B/notes/$NP {"title":"Renamed","description":"New text"}` | `200`, new title and description; `isFree` unchanged |
| 10b.8b | Owner: `PATCH $B/notes/$NP {"title":"  "}` | `400` title is required |
| 10b.9 | Owner: create a quiz on `L1` with `"isFree":true` and one without | `201` both |
| 10b.10 | `P`: `GET $B/lessons/$L1/quizzes` | Both quizzes; the paid one has `locked: true` |
| 10b.11 | `P`: `GET $B/quizzes/<paid quiz>` | `404` |
| 10b.12 | `GET $B/catalog/courses` | `freeNoteCount` counts `NF` |

---

## 11. `POST /auth/logout` (new)

No login is required, so it works even with an expired session.

| # | Request | Expect |
|---|---------|--------|
| 11.1 | `curl -s -D - -b admin.jar -c admin.jar -X POST $B/auth/logout` | `200`, two `Set-Cookie` headers: `access_token=; Path=/; Max-Age=0` and `refresh_token=; Path=/; Max-Age=0` (Go writes `MaxAge: -1` as `Max-Age=0`; browsers treat them the same) |
| 11.2 | `curl -s -b admin.jar $B/auth/me` | `401` |

Tokens are stateless JWTs. A token copied before logout stays valid until it expires. Revoking tokens is a possible future addition.

---

## 12. Fixes to existing problems

| # | Problem | Fix | How to test |
|---|---------|-----|-------------|
| 12.1 | An enrolled student could read lessons, notes and quizzes of a **draft or archived** course | `CourseAccess` now counts an enrollment only when the course is `published` | Enroll a student in a published course, set it to `draft`, then student `GET /courses/{id}/lessons` → `403` (was `200`). The owner still gets `200` |
| 12.2 | `POST /auth/refresh` crashed (nil pointer) when the user had been deleted, or when the token had no `sub` | Checks for a nil user and reads `sub` safely | Log in as a user, delete them in SQL, then `POST /auth/refresh` with their jar → `400` "user not found"; the server stays up |
| 12.3 | A soft-deleted course kept its slug forever | Migration 25 replaces the `UNIQUE(slug)` constraint with a unique index over non-deleted courses only | Test 8.5 |

---

## 13. Regression checks (existing endpoints)

Quick checks that existing behaviour still works:

- Instructor: create a lesson, quiz, post and note on their own course; change course status → all succeed.
- Instructor on another instructor's course → `403`/`404` as before.
- Student: `GET /courses/{id}/lessons`, `GET /quizzes/{id}` (no `isCorrect` or explanations), submit an attempt, `GET /quizzes/{id}/attempts`.
- `GET /posts` feed for student, instructor and admin.
- Admin: `GET /courses` (all courses), enrollment create, list and update.
- `POST /auth/refresh` with a valid session → `202`.

## 14. Video upload + transcoding (new)

Requires `GCS_ENABLE=true`, `GCP_PROJECT_ID`, `TRANSCODER_LOCATION` (e.g. `asia-south1`, same region as the bucket).

**One-time GCP setup**

- Enable the Transcoder API.
- API service account: `roles/transcoder.admin` plus object create/view on the bucket. Signing uses ADC (`GOOGLE_APPLICATION_CREDENTIALS` key, or `roles/iam.serviceAccountTokenCreator` on Cloud Run).
- Transcoder service agent `service-<PROJECT_NUMBER>@gcp-sa-transcoder.iam.gserviceaccount.com`: `roles/storage.objectAdmin` on the bucket.
- Bucket CORS: allow `PUT` from the frontend origin with headers `Content-Type`, `x-goog-content-length-range`.

**Flow** (instructor token)

1. `POST /lessons/{id}/videos` with `{"fileName":"lecture.mp4","isFree":false}` → `201` with `video`, `uploadUrl`, `method`, `headers`, `expiresAt`.
2. Send the returned `headers` unchanged (`Content-Type` follows the extension: mp4 `video/mp4`, mov `video/quicktime`, mkv `video/x-matroska`, webm `video/webm`):
   `curl -X PUT -H "Content-Type: video/mp4" -H "x-goog-content-length-range: 0,5368709120" --upload-file sample.mp4 "<uploadUrl>"`
3. `POST /videos/{id}/confirm` → `202`, status `processing`.
4. Poll `GET /videos/{id}` until `ready`. If it ends `failed`, fix the cause and `POST /videos/{id}/retry` → `202` (reuses the uploaded file; `409` if not failed).
    Check `gs://<bucket>/courses/<courseId>/videos/<videoId>/hls/manifest.m3u8` exists.

**Negative checks**

- Student → `403`. Other instructor's lesson/video → `404`.
- Confirm before PUT → `400` "video file not uploaded yet". Confirm twice → `409`.
- `fileName` ending `.exe` → `400`.
- PUT with wrong `Content-Type` → GCS `403`.
- `GCS_ENABLE=false` → `503`.
- Restart the server mid-transcode → video still reaches `ready` (same job is polled).

### Streaming (signed Cloud CDN URLs)

Requires `CDN_DOMAIN`, `CDN_KEY_NAME`, `CDN_SIGNING_KEY`. CDN backend bucket = `GCS_BUCKET_NAME` (stays private). Apply CORS: `gcloud storage buckets update gs://$GCS_BUCKET_NAME --cors-file=cors.json`, then invalidate CDN cache for `/courses/*`.

- `GET /videos/{id}/stream` → `{manifestUrl, queryParams, expiresAt}` (15 min, `Cache-Control: no-store`).
- `GET /videos/{id}/hls/{file}.m3u8?<signature>` → public (no cookie), any origin. Checks the signature, reads the playlist from the bucket and rewrites every line to a signed URL: child playlists point back at this route, `.ts` segments go straight to the CDN. Bad/expired signature → `403`.
- Access: free video (`videos.is_free` or `lessons.is_free`) in published course + published lesson → any logged-in user. Else course owner or enrolled student. Students never see non-ready videos or unpublished lessons (`404`). Owner streaming a non-ready video → `409`.

```
curl -b student.jar $B/videos/$VID/stream
curl "$manifestUrl"                 # 200 m3u8, every URL inside already signed
curl "<any URL from that file>"     # 200 (playlist) or 206/200 (segment)
curl "${manifestUrl%%\?*}"          # no signature → 403
```

`manifestUrl` plays as-is anywhere: browser address bar, Safari/iPhone, VLC, hls.js with no options, third-party demo players. hls.js:

```js
const s = await (await fetch(`/api/v1/videos/${id}/stream`, {credentials: "include"})).json().then(r => r.data);
const hls = new Hls();
hls.loadSource(s.manifestUrl);
hls.attachMedia(video);
// refetch /stream before s.expiresAt or on a 403, then loadSource again at the current time
```

### Resume where the student stopped

Requires migration `27_create_table_video_progress` (`make migrate-up`).

- `PUT /videos/{id}/progress {"positionSec": 754}` → `200 {positionSec}`. Same access rule as `/stream`. `400` if not 0–86400. Frontend calls it every 10–15 s while playing and on pause; send `0` when the video ends so the next play starts over.
- `GET /videos/{id}/stream` now also returns `resumeAt` (seconds). When it is > 0, `manifestUrl` ends with `&start=754` and the API adds `#EXT-X-START:TIME-OFFSET=754,PRECISE=YES` to `manifest.m3u8`, so hls.js and Safari start there by themselves. In your own player you can also use `new Hls({startPosition: s.resumeAt})`.

```
curl -b student.jar -X PUT -H 'Content-Type: application/json' -d '{"positionSec":754}' $B/videos/$VID/progress
curl -b student.jar $B/videos/$VID/stream          # resumeAt 754, manifestUrl ...&start=754
curl -s "$manifestUrl" | head -2                    # #EXTM3U / #EXT-X-START:TIME-OFFSET=754,PRECISE=YES
```

Known limits: videos stuck in `uploading` are not cleaned up (add a bucket lifecycle rule); `duration_sec` stays NULL; `CDN_DOMAIN` as plain `http://` exposes the signed query on the network, and an `https://` page will block `http://` segments (mixed content), so use HTTPS for both API and CDN in prod.

## Migrations added

| File | Change | Down |
|------|--------|------|
| `24_quizzes_soft_delete` | `quizzes.deleted_at` column | Drops it |
| `25_courses_slug_unique_active` | Slug unique only among non-deleted courses | Restores `courses_slug_key`. Fails if a deleted course and a live course share a slug |
| `26_videos_lessons_and_transcoding` | `videos.lesson_id`, `is_free`, `transcode_job` + index | Drops them |
| `27_create_table_video_progress` | `video_progress(user_id, video_id, position_sec)` for resume | Drops the table |
| `29_quiz_type` | `quizzes.type` (`mock_test`/`practice`), nullable `pass_percent` | Drops the column; null pass percents become 70 |
| `30_notes_is_free` | `notes.is_free` for per-note free preview | Drops the column; notes are free again only through their lesson |
