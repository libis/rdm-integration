data simple_data;
  input ID AGE GENDER $ INCOME EDUCATION;
  label ID='Respondent ID'
        AGE='Age in years'
        GENDER='Gender'
        INCOME='Annual income in dollars'
        EDUCATION='Education level';
  format GENDER $10.;
  datalines;
1 25 Male 35000 3
2 30 Female 42000 4
3 45 Male 55000 5
4 28 Female 38000 3
5 52 Male 68000 5
6 33 Female 45000 4
7 41 Male 52000 4
8 29 Female 36000 3
9 38 Male 48000 4
10 55 Female 72000 5
;
run;

proc format;
  value edufmt
    1='Less than high school'
    2='High school'
    3='Some college'
    4='Bachelor degree'
    5='Graduate degree';
run;
