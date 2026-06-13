//DSNJOB   JOB (ACCT),'MULTI DSN',CLASS=A,MSGCLASS=X
//*
//* #5043 — concatenated DDs, GDG relative generations, PDS members.
//*
//LOADSTEP EXEC PGM=LOADER
//*  Concatenated input DD: one logical DD (INDD) reading three datasets.
//INDD     DD DSN=PROD.PART.ONE,DISP=SHR
//         DD DSN=PROD.PART.TWO,DISP=SHR
//         DD DSN=PROD.PART.THREE,DISP=SHR
//*  GDG relative generations.
//GDGIN    DD DSN=PROD.HISTORY.GDG(-1),DISP=SHR
//GDGOUT   DD DSN=PROD.HISTORY.GDG(+1),DISP=(NEW,CATLG)
//*  PDS member granularity (library + member).
//PGMLIB   DD DSN=PROD.LOADLIB(PAYROLL),DISP=SHR
//SYSOUT   DD SYSOUT=*
//
