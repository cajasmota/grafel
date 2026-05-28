      ******************************************************************
      * EMPREC.CPY — shared employee-record copybook (data only).      *
      * Copybooks are the COBOL analog of an include/import unit; this *
      * one carries no PROCEDURE DIVISION, only WORKING-STORAGE items. *
      ******************************************************************
       01  EMPLOYEE-MASTER.
           05  EM-ID             PIC X(06).
           05  EM-DEPT           PIC X(04).
           05  EM-SALARY         PIC 9(07)V99.
           05  EM-STATUS         PIC X(01).
               88  EM-ACTIVE     VALUE 'A'.
               88  EM-TERMINATED VALUE 'T'.
