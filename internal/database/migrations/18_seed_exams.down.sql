DELETE FROM exams
WHERE code IN ('IFC', 'CSC-1', 'CSC-2', 'CIRE', 'RSE', 'LLQP')
  AND NOT EXISTS (SELECT 1 FROM courses WHERE courses.exam_id = exams.id);
