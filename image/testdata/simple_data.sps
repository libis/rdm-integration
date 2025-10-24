DATA LIST FREE / ID AGE GENDER INCOME EDUCATION.
BEGIN DATA
1 25 1 35000 3
2 30 2 42000 4
3 45 1 55000 5
4 28 2 38000 3
5 52 1 68000 5
6 33 2 45000 4
7 41 1 52000 4
8 29 2 36000 3
9 38 1 48000 4
10 55 2 72000 5
END DATA.

VARIABLE LABELS
  ID 'Respondent ID'
  AGE 'Age in years'
  GENDER 'Gender'
  INCOME 'Annual income in dollars'
  EDUCATION 'Education level'.

VALUE LABELS
  GENDER 1 'Male' 2 'Female' /
  EDUCATION 1 'Less than high school' 
            2 'High school' 
            3 'Some college' 
            4 'Bachelor degree'
            5 'Graduate degree'.

MISSING VALUES AGE INCOME (-99).

SAVE OUTFILE='simple_data.sav'.
