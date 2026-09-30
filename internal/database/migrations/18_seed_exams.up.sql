INSERT INTO exams (code, name) VALUES
    ('IFC',   'Investment Funds in Canada'),
    ('CSC-1', 'Canadian Securities Course, Volume 1'),
    ('CSC-2', 'Canadian Securities Course, Volume 2'),
    ('CIRE',  'Canadian Investment Regulations Exam'),
    ('RSE',   'Registered Supervisory Exam'),
    ('LLQP',  'Life License Qualification Program')
ON CONFLICT (code) DO NOTHING;
