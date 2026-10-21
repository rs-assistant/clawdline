package store

// Attribution uses durable identities, not directory basenames. The same CTE
// serves tokens and task cohorts, so the project picker cannot split them.
// UNION makes ancestry and path cycles terminate without inventing a depth cap.
// Conflicting terminal projects are deliberately unattributed.
const usageProjectSQL = `WITH RECURSIVE
 task_sources AS (
 SELECT id,assistant,json_extract(record,'$.child_conversation_id') conversation,
 COALESCE(NULLIF(repository,''),NULLIF(json_extract(record,'$.worktree.repository'),''),project) project,
 json_extract(record,'$.worktree.path') worktree,project cwd
 FROM broker_tasks
 ),
 path_seeds(path,project) AS (
 SELECT project,project_key FROM usage_daily WHERE project<>'' AND project_key<>''
 UNION SELECT worktree,project FROM task_sources WHERE worktree<>'' AND project<>''
 UNION SELECT cwd,project FROM task_sources WHERE cwd<>'' AND project<>''
 ),
 edges AS (SELECT path,project FROM path_seeds WHERE path<>project),
 paths(path,project) AS (
 SELECT path,project FROM path_seeds
 UNION SELECT p.path,e.project FROM paths p JOIN edges e ON e.path=p.project
 ),
 resolved_paths AS (
 SELECT path,CASE WHEN COUNT(DISTINCT project)=1 THEN MIN(project) ELSE '' END project
 FROM paths p WHERE NOT EXISTS (SELECT 1 FROM edges e WHERE e.path=p.project)
 GROUP BY path
 ),
 task_projects AS (
 SELECT t.id,t.assistant,t.conversation,
 CASE WHEN EXISTS(SELECT 1 FROM path_seeds s WHERE s.path=t.project)
 THEN COALESCE(p.project,'') ELSE t.project END project
 FROM task_sources t LEFT JOIN resolved_paths p ON p.path=t.project
 ),
 conversation_projects AS (
 SELECT assistant,conversation,CASE WHEN COUNT(DISTINCT project)=1 THEN MIN(project) ELSE '' END project
 FROM task_projects WHERE conversation<>'' GROUP BY assistant,conversation
 )`
